package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const listPageSize = 30

// blockerSummary is a compact view of a blocker task for blocked_by resolution.
type blockerSummary struct {
	ID            uuid.UUID `json:"id"`
	Subject       string    `json:"subject"`
	Status        string    `json:"status"`
	OwnerAgentKey string    `json:"owner_agent_key,omitempty"`
}

// taskListItem is the slim view returned by list/search actions.
type taskListItem struct {
	ID                uuid.UUID        `json:"id"`
	TaskNumber        int              `json:"task_number"`
	Identifier        string           `json:"identifier"`
	Subject           string           `json:"subject"`
	Status            string           `json:"status"`
	OwnerAgentID      *uuid.UUID       `json:"owner_agent_id,omitempty"`
	OwnerAgentKey     string           `json:"owner_agent_key,omitempty"`
	CreatedByAgentID  *uuid.UUID       `json:"created_by_agent_id,omitempty"`
	CreatedByAgentKey string           `json:"created_by_agent_key,omitempty"`
	ProgressPercent   int              `json:"progress_percent,omitempty"`
	ProgressStep      string           `json:"progress_step,omitempty"`
	BlockedBy         []blockerSummary `json:"blocked_by,omitempty"`
	CreatedAt         time.Time        `json:"created_at"`
}

// taskDetailItem is the slim view returned by the get action.
type taskDetailItem struct {
	ID                uuid.UUID        `json:"id"`
	TaskNumber        int              `json:"task_number"`
	Identifier        string           `json:"identifier"`
	Subject           string           `json:"subject"`
	Description       string           `json:"description,omitempty"`
	Status            string           `json:"status"`
	Result            *string          `json:"result,omitempty"`
	OwnerAgentID      *uuid.UUID       `json:"owner_agent_id,omitempty"`
	OwnerAgentKey     string           `json:"owner_agent_key,omitempty"`
	CreatedByAgentID  *uuid.UUID       `json:"created_by_agent_id,omitempty"`
	CreatedByAgentKey string           `json:"created_by_agent_key,omitempty"`
	ProgressPercent   int              `json:"progress_percent,omitempty"`
	ProgressStep      string           `json:"progress_step,omitempty"`
	BlockedBy         []blockerSummary `json:"blocked_by,omitempty"`
	Priority          int              `json:"priority"`
	CreatedAt         time.Time        `json:"created_at"`
	UpdatedAt         time.Time        `json:"updated_at"`
}

// slimComment is the slim comment view for get response.
type slimComment struct {
	AgentKey  string    `json:"agent_key"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

// slimEvent is the slim event view for get response.
type slimEvent struct {
	EventType string    `json:"event_type"`
	ActorID   string    `json:"actor_id"`
	CreatedAt time.Time `json:"created_at"`
}

// teamCreateLocks serializes list→create flows per (teamID:chatID) pair.
var teamCreateLocks sync.Map // key: "teamID:chatID" → *sync.Mutex

func getTeamCreateLock(teamID, chatID string) *sync.Mutex {
	key := teamID + ":" + chatID
	v, _ := teamCreateLocks.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// resolveBlockers batch-loads blocker tasks and returns slim summaries.
func (t *TeamTasksTool) resolveBlockers(ctx context.Context, blockedBy []uuid.UUID) []blockerSummary {
	if len(blockedBy) == 0 {
		return nil
	}
	tasks, err := t.manager.teamStore.GetTasksByIDs(ctx, blockedBy)
	if err != nil {
		return nil
	}
	out := make([]blockerSummary, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, blockerSummary{
			ID:            task.ID,
			Subject:       task.Subject,
			Status:        task.Status,
			OwnerAgentKey: task.OwnerAgentKey,
		})
	}
	return out
}

func (t *TeamTasksTool) toListItem(ctx context.Context, task store.TeamTaskData) taskListItem {
	return taskListItem{
		ID:                task.ID,
		TaskNumber:        task.TaskNumber,
		Identifier:        task.Identifier,
		Subject:           task.Subject,
		Status:            task.Status,
		OwnerAgentID:      task.OwnerAgentID,
		OwnerAgentKey:     task.OwnerAgentKey,
		CreatedByAgentID:  task.CreatedByAgentID,
		CreatedByAgentKey: task.CreatedByAgentKey,
		ProgressPercent:   task.ProgressPercent,
		ProgressStep:      task.ProgressStep,
		BlockedBy:         t.resolveBlockers(ctx, task.BlockedBy),
		CreatedAt:         task.CreatedAt,
	}
}

func (t *TeamTasksTool) toDetailItem(ctx context.Context, task *store.TeamTaskData) taskDetailItem {
	return taskDetailItem{
		ID:                task.ID,
		TaskNumber:        task.TaskNumber,
		Identifier:        task.Identifier,
		Subject:           task.Subject,
		Description:       task.Description,
		Status:            task.Status,
		Result:            task.Result,
		OwnerAgentID:      task.OwnerAgentID,
		OwnerAgentKey:     task.OwnerAgentKey,
		CreatedByAgentID:  task.CreatedByAgentID,
		CreatedByAgentKey: task.CreatedByAgentKey,
		ProgressPercent:   task.ProgressPercent,
		ProgressStep:      task.ProgressStep,
		BlockedBy:         t.resolveBlockers(ctx, task.BlockedBy),
		Priority:          task.Priority,
		CreatedAt:         task.CreatedAt,
		UpdatedAt:         task.UpdatedAt,
	}
}

func (t *TeamTasksTool) executeList(ctx context.Context, args map[string]any) *Result {
	team, _, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	statusFilter, _ := args["status"].(string)

	page := 1
	if p, ok := args["page"].(float64); ok && int(p) > 1 {
		page = int(p)
	}
	offset := (page - 1) * listPageSize

	// Delegate/system channels see all tasks; end users only see their own.
	filterUserID := ""
	channel := ToolChannelFromCtx(ctx)
	if channel != ChannelDelegate && channel != ChannelSystem {
		filterUserID = store.UserIDFromContext(ctx)
	}
	chatID := ToolChatIDFromCtx(ctx)
	// Shared workspace: show all tasks across chats.
	listChatID := chatID
	if IsSharedWorkspace(team.Settings) {
		listChatID = ""
	}

	// Acquire team create lock to serialize list→create flows across concurrent goroutines.
	if ptd := PendingTeamDispatchFromCtx(ctx); ptd != nil && !ptd.HasListed() {
		lock := getTeamCreateLock(team.ID.String(), chatID)
		lock.Lock()
		ptd.SetTeamLock(lock)
		ptd.MarkListed()
	}

	tasks, err := t.manager.teamStore.ListTasks(ctx, team.ID, "priority", statusFilter, filterUserID, "", listChatID, offset)
	if err != nil {
		return ErrorResult("failed to list tasks: " + err.Error())
	}

	hasMore := len(tasks) > listPageSize
	if hasMore {
		tasks = tasks[:listPageSize]
	}

	items := make([]taskListItem, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, t.toListItem(ctx, task))
	}

	resp := map[string]any{
		"tasks": items,
		"count": len(items),
		"page":  page,
	}
	if hasMore {
		resp["has_more"] = true
	}

	out, _ := json.Marshal(resp)
	return SilentResult(string(out))
}

// resolveTaskID extracts and validates the task_id from tool arguments.
// Falls back to the dispatched task ID from context when task_id is empty or
// not a valid UUID (agents often pass task_number like "1" instead of the UUID).
func resolveTaskID(ctx context.Context, args map[string]any) (uuid.UUID, error) {
	taskIDStr, _ := args["task_id"].(string)

	// Try parsing as UUID first.
	if taskIDStr != "" {
		if id, err := uuid.Parse(taskIDStr); err == nil {
			return id, nil
		}
	}

	// Fall back to the dispatched team task ID from context.
	if ctxID := TeamTaskIDFromCtx(ctx); ctxID != "" {
		if id, err := uuid.Parse(ctxID); err == nil {
			return id, nil
		}
	}

	if taskIDStr == "" {
		return uuid.Nil, fmt.Errorf("task_id is required")
	}
	return uuid.Nil, fmt.Errorf("invalid task_id %q — use the UUID from task list, not the task number", taskIDStr)
}

func (t *TeamTasksTool) executeGet(ctx context.Context, args map[string]any) *Result {
	team, _, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskID, err := resolveTaskID(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	task, err := t.manager.teamStore.GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("failed to get task: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}

	// Truncate result for context protection
	const maxResultRunes = 8000
	if task.Result != nil {
		r := []rune(*task.Result)
		if len(r) > maxResultRunes {
			s := string(r[:maxResultRunes]) + "..."
			task.Result = &s
		}
	}

	detail := t.toDetailItem(ctx, task)

	// Load and slim comments/events/attachments
	resp := map[string]any{"task": detail}

	if comments, _ := t.manager.teamStore.ListTaskComments(ctx, taskID); len(comments) > 0 {
		slim := make([]slimComment, 0, len(comments))
		for _, c := range comments {
			key := ""
			if c.AgentID != nil {
				key = t.manager.agentKeyFromID(ctx, *c.AgentID)
			}
			slim = append(slim, slimComment{
				AgentKey:  key,
				Content:   c.Content,
				CreatedAt: c.CreatedAt,
			})
		}
		resp["comments"] = slim
	}

	if events, _ := t.manager.teamStore.ListTaskEvents(ctx, taskID); len(events) > 0 {
		slim := make([]slimEvent, 0, len(events))
		for _, e := range events {
			slim = append(slim, slimEvent{
				EventType: e.EventType,
				ActorID:   e.ActorID,
				CreatedAt: e.CreatedAt,
			})
		}
		resp["events"] = slim
	}

	if attachments, _ := t.manager.teamStore.ListTaskAttachments(ctx, taskID); len(attachments) > 0 {
		resp["attachments"] = attachments
	}

	out, _ := json.Marshal(resp)
	return SilentResult(string(out))
}

func (t *TeamTasksTool) executeSearch(ctx context.Context, args map[string]any) *Result {
	team, _, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	query, _ := args["query"].(string)
	if query == "" {
		return ErrorResult("query is required for search action")
	}

	// Delegate/system channels see all tasks; end users only see their own.
	filterUserID := ""
	channel := ToolChannelFromCtx(ctx)
	if channel != ChannelDelegate && channel != ChannelSystem {
		filterUserID = store.UserIDFromContext(ctx)
	}

	tasks, err := t.manager.teamStore.SearchTasks(ctx, team.ID, query, listPageSize, filterUserID)
	if err != nil {
		return ErrorResult("failed to search tasks: " + err.Error())
	}

	items := make([]taskListItem, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, t.toListItem(ctx, task))
	}

	out, _ := json.Marshal(map[string]any{
		"tasks": items,
		"count": len(items),
	})
	return SilentResult(string(out))
}

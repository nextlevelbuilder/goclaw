package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const listPageSize    = 30
const searchPageSize  = 5

// blockerSummary is a compact view of a blocker task for blocked_by resolution.
type blockerSummary struct {
	ID            uuid.UUID `json:"id"`
	Subject       string    `json:"subject"`
	Status        string    `json:"status"`
	OwnerAgentKey string    `json:"owner_agent_key,omitempty"`
}

// taskListItem is the slim view returned by list/search actions.
// Excludes UUIDs (owner_agent_id, created_by_agent_id) and task_number — model uses
// agent keys and identifier instead.
type taskListItem struct {
	ID                   uuid.UUID        `json:"id"`
	Identifier           string           `json:"identifier"`
	Subject              string           `json:"subject"`
	Status               string           `json:"status"`
	OwnerAgentKey        string           `json:"owner_agent_key,omitempty"`
	OwnerDisplayName     string           `json:"owner_display_name,omitempty"`
	CreatedByAgentKey    string           `json:"created_by_agent_key,omitempty"`
	CreatedByDisplayName string           `json:"created_by_display_name,omitempty"`
	ProgressPercent      int              `json:"progress_percent,omitempty"`
	ProgressStep         string           `json:"progress_step,omitempty"`
	BlockedBy            []blockerSummary `json:"blocked_by,omitempty"`
	CreatedAt            time.Time        `json:"created_at"`
}

// taskDetailItem is the slim view returned by the get action.
type taskDetailItem struct {
	ID                   uuid.UUID        `json:"id"`
	Identifier           string           `json:"identifier"`
	Subject              string           `json:"subject"`
	Description          string           `json:"description,omitempty"`
	Status               string           `json:"status"`
	Result               *string          `json:"result,omitempty"`
	OwnerAgentKey        string           `json:"owner_agent_key,omitempty"`
	OwnerDisplayName     string           `json:"owner_display_name,omitempty"`
	CreatedByAgentKey    string           `json:"created_by_agent_key,omitempty"`
	CreatedByDisplayName string           `json:"created_by_display_name,omitempty"`
	ProgressPercent      int              `json:"progress_percent,omitempty"`
	ProgressStep         string           `json:"progress_step,omitempty"`
	BlockedBy            []blockerSummary `json:"blocked_by,omitempty"`
	Priority             int              `json:"priority"`
	CreatedAt            time.Time        `json:"created_at"`
	UpdatedAt            time.Time        `json:"updated_at"`
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
	tasks, err := t.manager.Store().GetTasksByIDs(ctx, blockedBy)
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
		ID:                   task.ID,
		Identifier:           task.Identifier,
		Subject:              task.Subject,
		Status:               task.Status,
		OwnerAgentKey:        task.OwnerAgentKey,
		OwnerDisplayName:     t.manager.AgentDisplayName(ctx, task.OwnerAgentKey),
		CreatedByAgentKey:    task.CreatedByAgentKey,
		CreatedByDisplayName: t.manager.AgentDisplayName(ctx, task.CreatedByAgentKey),
		ProgressPercent:      task.ProgressPercent,
		ProgressStep:         task.ProgressStep,
		BlockedBy:            t.resolveBlockers(ctx, task.BlockedBy),
		CreatedAt:            task.CreatedAt,
	}
}

func (t *TeamTasksTool) toDetailItem(ctx context.Context, task *store.TeamTaskData) taskDetailItem {
	return taskDetailItem{
		ID:                   task.ID,
		Identifier:           task.Identifier,
		Subject:              task.Subject,
		Description:          task.Description,
		Status:               task.Status,
		Result:               task.Result,
		OwnerAgentKey:        task.OwnerAgentKey,
		OwnerDisplayName:     t.manager.AgentDisplayName(ctx, task.OwnerAgentKey),
		CreatedByAgentKey:    task.CreatedByAgentKey,
		CreatedByDisplayName: t.manager.AgentDisplayName(ctx, task.CreatedByAgentKey),
		ProgressPercent:      task.ProgressPercent,
		ProgressStep:         task.ProgressStep,
		BlockedBy:            t.resolveBlockers(ctx, task.BlockedBy),
		Priority:             task.Priority,
		CreatedAt:            task.CreatedAt,
		UpdatedAt:            task.UpdatedAt,
	}
}

// buildCreateHint generates task creation guidance with member+model info.
// Injected into search/list results so weaker models (MiniMax, Qwen) get
// actionable hints before calling create.
func (t *TeamTasksTool) buildCreateHint(ctx context.Context, teamID, leadAgentID, callerAgentID uuid.UUID) string {
	members, err := t.manager.CachedListMembers(ctx, teamID, callerAgentID)
	if err != nil || len(members) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[Task creation guide]\nAvailable members and their models:\n")
	for _, m := range members {
		if m.AgentID == leadAgentID {
			continue // skip lead from member list
		}
		model := ""
		if ag, err := t.manager.CachedGetAgentByID(ctx, m.AgentID); err == nil {
			model = ag.Model
		}
		entry := fmt.Sprintf("- %s (%s)", m.AgentKey, model)
		if m.Frontmatter != "" {
			fm := m.Frontmatter
			if len([]rune(fm)) > 80 {
				fm = string([]rune(fm)[:80]) + "…"
			}
			entry += " — " + fm
		}
		sb.WriteString(entry + "\n")
	}
	sb.WriteString("\nBefore creating a task, check:\n")
	sb.WriteString("1. SKILL TEST: Does task need 2+ different skills (research+writing, design+coding)? → Split into separate tasks with blocked_by\n")
	sb.WriteString("2. SCOPE TEST: Could a member finish this in one focused session? If not, break it down\n")
	sb.WriteString("3. SELF-CONTAINED: Include objective, context, constraints, expected output. Description can be detailed — that's OK. Member only sees this\n")
	sb.WriteString("4. MODEL MATCH: Complex reasoning → stronger model. Simple tasks → any model")
	return sb.String()
}

func (t *TeamTasksTool) executeList(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.ResolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	statusFilter, _ := args["status"].(string)

	page := 1
	if p, ok := args["page"].(float64); ok && int(p) > 1 {
		page = int(p)
	}
	offset := (page - 1) * listPageSize

	// Teammate/system channels see all tasks; end users only see their own.
	filterUserID := ""
	channel := ToolChannelFromCtx(ctx)
	if channel != ChannelTeammate && channel != ChannelSystem {
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

	tasks, err := t.manager.Store().ListTasks(ctx, team.ID, "priority", statusFilter, filterUserID, "", listChatID, 0, offset)
	if err != nil {
		return ErrorResult("failed to list tasks: " + err.Error())
	}

	hasMore := len(tasks) > listPageSize
	if hasMore {
		tasks = tasks[:listPageSize]
	}

	// Pre-warm agent cache to avoid N+1 queries for display names.
	agentKeys := make([]string, 0, len(tasks)*2)
	for _, task := range tasks {
		agentKeys = append(agentKeys, task.OwnerAgentKey, task.CreatedByAgentKey)
	}
	t.manager.PreWarmAgentKeyCache(ctx, agentKeys)

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
	if hint := t.buildCreateHint(ctx, team.ID, team.LeadAgentID, agentID); hint != "" {
		resp["hint"] = hint
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
	team, _, err := t.manager.ResolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskID, err := resolveTaskID(ctx, args)
	if err != nil {
		return ErrorResult(err.Error())
	}

	task, err := t.manager.Store().GetTask(ctx, taskID)
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

	// Pre-warm cache for task owner + creator display names.
	t.manager.PreWarmAgentKeyCache(ctx, []string{task.OwnerAgentKey, task.CreatedByAgentKey})

	detail := t.toDetailItem(ctx, task)

	// Load and slim comments/events/attachments
	resp := map[string]any{"task": detail}

	if comments, _ := t.manager.Store().ListTaskComments(ctx, taskID); len(comments) > 0 {
		// Pre-warm agent cache to avoid N+1 queries for comment agent keys.
		commentAgentIDs := make([]uuid.UUID, 0, len(comments))
		for _, c := range comments {
			if c.AgentID != nil {
				commentAgentIDs = append(commentAgentIDs, *c.AgentID)
			}
		}
		t.manager.PreWarmAgentIDCache(ctx, commentAgentIDs)

		slim := make([]slimComment, 0, len(comments))
		for _, c := range comments {
			key := ""
			if c.AgentID != nil {
				key = t.manager.AgentKeyFromID(ctx, *c.AgentID)
			}
			slim = append(slim, slimComment{
				AgentKey:  key,
				Content:   c.Content,
				CreatedAt: c.CreatedAt,
			})
		}
		resp["comments"] = slim
	}

	if events, _ := t.manager.Store().ListTaskEvents(ctx, taskID); len(events) > 0 {
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

	if attachments, _ := t.manager.Store().ListTaskAttachments(ctx, taskID); len(attachments) > 0 {
		resp["attachments"] = attachments
	}

	out, _ := json.Marshal(resp)
	return SilentResult(string(out))
}

func (t *TeamTasksTool) executeSearch(ctx context.Context, args map[string]any) *Result {
	team, agentID, err := t.manager.ResolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	query, _ := args["query"].(string)
	if query == "" {
		return ErrorResult("query is required for search action")
	}

	// Teammate/system channels see all tasks; end users only see their own.
	filterUserID := ""
	channel := ToolChannelFromCtx(ctx)
	if channel != ChannelTeammate && channel != ChannelSystem {
		filterUserID = store.UserIDFromContext(ctx)
	}

	// Acquire team create lock so search also satisfies the list-before-create gate.
	chatID := ToolChatIDFromCtx(ctx)
	if ptd := PendingTeamDispatchFromCtx(ctx); ptd != nil && !ptd.HasListed() {
		lock := getTeamCreateLock(team.ID.String(), chatID)
		lock.Lock()
		ptd.SetTeamLock(lock)
		ptd.MarkListed()
	}

	tasks, err := t.manager.Store().SearchTasks(ctx, team.ID, query, searchPageSize, filterUserID)
	if err != nil {
		return ErrorResult("failed to search tasks: " + err.Error())
	}

	items := make([]taskListItem, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, t.toListItem(ctx, task))
	}

	resp := map[string]any{
		"tasks": items,
		"count": len(items),
	}
	if hint := t.buildCreateHint(ctx, team.ID, team.LeadAgentID, agentID); hint != "" {
		resp["hint"] = hint
	}

	out, _ := json.Marshal(resp)
	return SilentResult(string(out))
}

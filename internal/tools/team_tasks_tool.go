package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/pkg/protocol"
)

// TeamTasksTool exposes the shared team task list to agents.
// Actions: list, get, create, claim, complete, search.
type TeamTasksTool struct {
	manager *TeamToolManager
}

func NewTeamTasksTool(manager *TeamToolManager) *TeamTasksTool {
	return &TeamTasksTool{manager: manager}
}

func (t *TeamTasksTool) Name() string { return "team_tasks" }

func (t *TeamTasksTool) Description() string {
	return "Manage the shared team task list. Actions: list (active tasks overview), get (full task detail with result), create, claim, complete, cancel, search. See TEAM.md for your team context."
}

func (t *TeamTasksTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"description": "'list', 'get', 'create', 'claim', 'complete', 'cancel', or 'search'",
			},
			"status": map[string]interface{}{
				"type":        "string",
				"description": "Filter for action=list: '' (active only, default), 'completed', 'all'",
			},
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query for action=search (searches subject and description)",
			},
			"subject": map[string]interface{}{
				"type":        "string",
				"description": "Task subject (required for action=create)",
			},
			"description": map[string]interface{}{
				"type":        "string",
				"description": "Task description (optional, for action=create)",
			},
			"priority": map[string]interface{}{
				"type":        "number",
				"description": "Task priority, higher = more important (optional, for action=create, default 0)",
			},
			"blocked_by": map[string]interface{}{
				"type":        "array",
				"items":       map[string]interface{}{"type": "string"},
				"description": "Task IDs that must complete before this task can be claimed (optional, for action=create)",
			},
			"task_id": map[string]interface{}{
				"type":        "string",
				"description": "Task ID (required for action=get, claim, complete, cancel)",
			},
			"result": map[string]interface{}{
				"type":        "string",
				"description": "Task result summary (required for action=complete)",
			},
			"reason": map[string]interface{}{
				"type":        "string",
				"description": "Cancellation reason (optional for action=cancel)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *TeamTasksTool) Execute(ctx context.Context, args map[string]interface{}) *Result {
	action, _ := args["action"].(string)

	switch action {
	case "list":
		return t.executeList(ctx, args)
	case "get":
		return t.executeGet(ctx, args)
	case "create":
		return t.executeCreate(ctx, args)
	case "claim":
		return t.executeClaim(ctx, args)
	case "complete":
		return t.executeComplete(ctx, args)
	case "cancel":
		return t.executeCancel(ctx, args)
	case "search":
		return t.executeSearch(ctx, args)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s (use list, get, create, claim, complete, cancel, or search)", action))
	}
}

const listTasksLimit = 20

func (t *TeamTasksTool) executeList(ctx context.Context, args map[string]interface{}) *Result {
	team, _, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	statusFilter, _ := args["status"].(string)

	// Delegate/system channels see all tasks; end users only see their own.
	filterUserID := ""
	channel := ToolChannelFromCtx(ctx)
	if channel != "delegate" && channel != "system" {
		filterUserID = store.UserIDFromContext(ctx)
	}

	tasks, err := t.manager.teamStore.ListTasks(ctx, team.ID, "priority", statusFilter, filterUserID)
	if err != nil {
		return ErrorResult("failed to list tasks: " + err.Error())
	}

	// Strip results from list view — use action=get for full detail
	for i := range tasks {
		tasks[i].Result = nil
	}

	hasMore := len(tasks) > listTasksLimit
	if hasMore {
		tasks = tasks[:listTasksLimit]
	}

	resp := map[string]interface{}{
		"tasks": tasks,
		"count": len(tasks),
	}
	if hasMore {
		resp["note"] = fmt.Sprintf("Showing first %d tasks. Use action=search with a query to find older tasks.", listTasksLimit)
		resp["has_more"] = true
	}

	out, _ := json.Marshal(resp)
	return SilentResult(string(out))
}

func (t *TeamTasksTool) executeGet(ctx context.Context, args map[string]interface{}) *Result {
	team, _, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for get action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
	}

	task, err := t.manager.teamStore.GetTask(ctx, taskID)
	if err != nil {
		return ErrorResult("failed to get task: " + err.Error())
	}
	if task.TeamID != team.ID {
		return ErrorResult("task does not belong to your team")
	}

	// Truncate result for context protection (full result in DB)
	const maxResultRunes = 8000
	if task.Result != nil {
		r := []rune(*task.Result)
		if len(r) > maxResultRunes {
			s := string(r[:maxResultRunes]) + "..."
			task.Result = &s
		}
	}

	out, _ := json.Marshal(task)
	return SilentResult(string(out))
}

func (t *TeamTasksTool) executeSearch(ctx context.Context, args map[string]interface{}) *Result {
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
	if channel != "delegate" && channel != "system" {
		filterUserID = store.UserIDFromContext(ctx)
	}

	tasks, err := t.manager.teamStore.SearchTasks(ctx, team.ID, query, 20, filterUserID)
	if err != nil {
		return ErrorResult("failed to search tasks: " + err.Error())
	}

	// Show result snippets in search results
	const maxSnippetRunes = 500
	for i := range tasks {
		if tasks[i].Result != nil {
			r := []rune(*tasks[i].Result)
			if len(r) > maxSnippetRunes {
				s := string(r[:maxSnippetRunes]) + "..."
				tasks[i].Result = &s
			}
		}
	}

	out, _ := json.Marshal(map[string]interface{}{
		"tasks": tasks,
		"count": len(tasks),
	})
	return SilentResult(string(out))
}

func (t *TeamTasksTool) executeCreate(ctx context.Context, args map[string]interface{}) *Result {
	team, _, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	subject, _ := args["subject"].(string)
	if subject == "" {
		return ErrorResult("subject is required for create action")
	}

	description, _ := args["description"].(string)
	priority := 0
	if p, ok := args["priority"].(float64); ok {
		priority = int(p)
	}

	var blockedBy []uuid.UUID
	if raw, ok := args["blocked_by"].([]interface{}); ok {
		for _, v := range raw {
			if s, ok := v.(string); ok {
				if id, err := uuid.Parse(s); err == nil {
					blockedBy = append(blockedBy, id)
				}
			}
		}
	}

	status := store.TeamTaskStatusPending
	if len(blockedBy) > 0 {
		status = store.TeamTaskStatusBlocked
	}

	task := &store.TeamTaskData{
		TeamID:      team.ID,
		Subject:     subject,
		Description: description,
		Status:      status,
		BlockedBy:   blockedBy,
		Priority:    priority,
		UserID:      store.UserIDFromContext(ctx),
		Channel:     ToolChannelFromCtx(ctx),
	}

	if err := t.manager.teamStore.CreateTask(ctx, task); err != nil {
		return ErrorResult("failed to create task: " + err.Error())
	}

	t.manager.broadcastTeamEvent(protocol.EventTeamTaskCreated, protocol.TeamTaskEventPayload{
		TeamID:    team.ID.String(),
		TaskID:    task.ID.String(),
		Subject:   subject,
		Status:    status,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    ToolChatIDFromCtx(ctx),
		Timestamp: task.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})

	return NewResult(fmt.Sprintf("Task created: %s (id=%s, status=%s)", subject, task.ID, status))
}

func (t *TeamTasksTool) executeClaim(ctx context.Context, args map[string]interface{}) *Result {
	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for claim action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
	}

	if err := t.manager.teamStore.ClaimTask(ctx, taskID, agentID, team.ID); err != nil {
		return ErrorResult("failed to claim task: " + err.Error())
	}

	ownerKey := t.manager.agentKeyFromID(ctx, agentID)
	t.manager.broadcastTeamEvent(protocol.EventTeamTaskClaimed, protocol.TeamTaskEventPayload{
		TeamID:           team.ID.String(),
		TaskID:           taskIDStr,
		Status:           store.TeamTaskStatusInProgress,
		OwnerAgentKey:    ownerKey,
		OwnerDisplayName: t.manager.agentDisplayName(ctx, ownerKey),
		UserID:           store.UserIDFromContext(ctx),
		Channel:          ToolChannelFromCtx(ctx),
		ChatID:           ToolChatIDFromCtx(ctx),
		Timestamp:        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})

	return NewResult(fmt.Sprintf("Task %s claimed successfully. It is now in progress.", taskIDStr))
}

func (t *TeamTasksTool) executeComplete(ctx context.Context, args map[string]interface{}) *Result {
	// Delegate agents cannot complete tasks — autoCompleteTeamTask handles it.
	if ToolChannelFromCtx(ctx) == "delegate" {
		return ErrorResult("delegate agents cannot complete team tasks directly — results are auto-completed when delegation finishes")
	}

	team, agentID, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for complete action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
	}

	result, _ := args["result"].(string)
	if result == "" {
		return ErrorResult("result is required for complete action")
	}

	// Auto-claim if the task is still pending (saves an extra tool call).
	// ClaimTask is atomic — only one agent can succeed, others get an error.
	// Ignore claim error: task may already be in_progress (claimed by us or someone else).
	_ = t.manager.teamStore.ClaimTask(ctx, taskID, agentID, team.ID)

	if err := t.manager.teamStore.CompleteTask(ctx, taskID, team.ID, result); err != nil {
		return ErrorResult("failed to complete task: " + err.Error())
	}

	ownerKey := t.manager.agentKeyFromID(ctx, agentID)
	t.manager.broadcastTeamEvent(protocol.EventTeamTaskCompleted, protocol.TeamTaskEventPayload{
		TeamID:           team.ID.String(),
		TaskID:           taskIDStr,
		Status:           store.TeamTaskStatusCompleted,
		OwnerAgentKey:    ownerKey,
		OwnerDisplayName: t.manager.agentDisplayName(ctx, ownerKey),
		UserID:           store.UserIDFromContext(ctx),
		Channel:          ToolChannelFromCtx(ctx),
		ChatID:           ToolChatIDFromCtx(ctx),
		Timestamp:        time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})

	return NewResult(fmt.Sprintf("Task %s completed. Dependent tasks have been unblocked.", taskIDStr))
}

func (t *TeamTasksTool) executeCancel(ctx context.Context, args map[string]interface{}) *Result {
	// Delegate agents cannot cancel tasks — only lead/user-facing agents can.
	if ToolChannelFromCtx(ctx) == "delegate" {
		return ErrorResult("delegate agents cannot cancel team tasks directly")
	}

	team, _, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	taskIDStr, _ := args["task_id"].(string)
	if taskIDStr == "" {
		return ErrorResult("task_id is required for cancel action")
	}
	taskID, err := uuid.Parse(taskIDStr)
	if err != nil {
		return ErrorResult("invalid task_id")
	}

	reason, _ := args["reason"].(string)
	if reason == "" {
		reason = "Cancelled by agent"
	}

	// CancelTask: guards against completed tasks, unblocks dependents, transitions blocked→pending.
	if err := t.manager.teamStore.CancelTask(ctx, taskID, team.ID, reason); err != nil {
		return ErrorResult("failed to cancel task: " + err.Error())
	}

	// Cancel any running delegation for this task.
	if t.manager.delegateMgr != nil {
		t.manager.delegateMgr.CancelByTeamTaskID(taskID)
	}

	t.manager.broadcastTeamEvent(protocol.EventTeamTaskCancelled, protocol.TeamTaskEventPayload{
		TeamID:    team.ID.String(),
		TaskID:    taskIDStr,
		Status:    "cancelled",
		Reason:    reason,
		UserID:    store.UserIDFromContext(ctx),
		Channel:   ToolChannelFromCtx(ctx),
		ChatID:    ToolChatIDFromCtx(ctx),
		Timestamp: time.Now().UTC().Format("2006-01-02T15:04:05Z"),
	})

	return NewResult(fmt.Sprintf("Task %s cancelled. Any running delegation has been stopped and dependent tasks unblocked.", taskIDStr))
}

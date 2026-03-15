package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const listTasksLimit = 20

func (t *TeamTasksTool) executeList(ctx context.Context, args map[string]any) *Result {
	team, _, err := t.manager.resolveTeam(ctx)
	if err != nil {
		return ErrorResult(err.Error())
	}

	statusFilter, _ := args["status"].(string)

	// Delegate/system channels see all tasks; end users only see their own.
	filterUserID := ""
	channel := ToolChannelFromCtx(ctx)
	if channel != ChannelDelegate && channel != ChannelSystem {
		filterUserID = store.UserIDFromContext(ctx)
	}

	tasks, err := t.manager.teamStore.ListTasks(ctx, team.ID, "priority", statusFilter, filterUserID, "", "")
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

	resp := map[string]any{
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

	// Truncate result for context protection (full result in DB)
	const maxResultRunes = 8000
	if task.Result != nil {
		r := []rune(*task.Result)
		if len(r) > maxResultRunes {
			s := string(r[:maxResultRunes]) + "..."
			task.Result = &s
		}
	}

	// Load comments, events, and attachments for full detail view.
	comments, _ := t.manager.teamStore.ListTaskComments(ctx, taskID)
	events, _ := t.manager.teamStore.ListTaskEvents(ctx, taskID)
	attachments, _ := t.manager.teamStore.ListTaskAttachments(ctx, taskID)

	resp := map[string]any{
		"task": task,
	}
	if len(comments) > 0 {
		resp["comments"] = comments
	}
	if len(events) > 0 {
		resp["events"] = events
	}
	if len(attachments) > 0 {
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

	out, _ := json.Marshal(map[string]any{
		"tasks": tasks,
		"count": len(tasks),
	})
	return SilentResult(string(out))
}

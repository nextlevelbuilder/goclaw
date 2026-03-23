package tools

import (
	"context"
	"fmt"
)

// TeamTasksTool exposes the shared team task list to agents.
// Actions: list, get, create, claim, complete, cancel, search, review, comment, progress, attach, update.
type TeamTasksTool struct {
	manager *TeamToolManager
}

func NewTeamTasksTool(manager *TeamToolManager) *TeamTasksTool {
	return &TeamTasksTool{manager: manager}
}

func (t *TeamTasksTool) Name() string { return "team_tasks" }

func (t *TeamTasksTool) Description() string {
	return "Manage the shared team task list (create, claim, complete, track progress). See TEAM.md for available actions and team context."
}

func (t *TeamTasksTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type": "string",
				"description": "'list', 'get', 'create', 'claim', 'complete', 'cancel', 'approve', 'reject', 'search', 'review', 'comment', 'progress', 'attach', 'update', 'ask_user', or 'clear_ask_user'. " +
					"ask_user: set a periodic reminder that is sent to the USER (not the team) when you need the user's input/decision to continue (e.g. 'Which design do you prefer?'). ONLY use when you have a question for the user. Do NOT use for status updates, waiting for teammates, or notifications — use 'progress' instead. " +
					"clear_ask_user: cancel a previously set ask_user reminder. " +
					"retry: re-dispatch a stale or failed task.",
			},
			"task_id": map[string]any{
				"type":        "string",
				"description": "Task UUID (required for most actions except list, create, search). When working on a dispatched task, this is auto-resolved from context — you can omit it for complete/progress/comment.",
			},
			"subject": map[string]any{
				"type":        "string",
				"description": "Task subject (required for create, optional for update)",
			},
			"description": map[string]any{
				"type":        "string",
				"description": "Task description with clear objectives and requirements. Include all context the assignee needs to complete the task independently.",
			},
			"result": map[string]any{
				"type":        "string",
				"description": "Result summary (required for complete)",
			},
			"text": map[string]any{
				"type":        "string",
				"description": "Text content: comment text, cancel/reject reason, progress update, or ask_user reminder question (must be a question asking the user for input/decision)",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "Comment type for action=comment: 'note' (default, share findings) or 'blocker' (you are BLOCKED and need leader input — auto-fails task and notifies leader)",
			},
			"status": map[string]any{
				"type":        "string",
				"description": "Filter for list: '' (all, default), 'active', 'completed', 'in_review'",
			},
			"query": map[string]any{
				"type":        "string",
				"description": "Search query for action=search (supports keyword AND semantic matching). Use search before create to check for duplicates.",
			},
			"priority": map[string]any{
				"type":        "number",
				"description": "Priority, higher = more important (for create, default 0)",
			},
			"blocked_by": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Task IDs that must complete first (for create/update)",
			},
			"require_approval": map[string]any{
				"type":        "boolean",
				"description": "Require user approval before claim (for create, default false)",
			},
			"percent": map[string]any{
				"type":        "integer",
				"description": "Progress percentage 0-100 (for progress action)",
			},
			"file_id": map[string]any{
				"type":        "string",
				"description": "Workspace file ID (for attach)",
			},
			"assignee": map[string]any{
				"type":        "string",
				"description": "Agent key to assign task to (REQUIRED for create). Auto-dispatches to that team member.",
			},
			"page": map[string]any{
				"type":        "number",
				"description": "Page number for list/search (default 1, 30 per page)",
			},
		},
		"required": []string{"action"},
	}
}

func (t *TeamTasksTool) Execute(ctx context.Context, args map[string]any) *Result {
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
	case "approve":
		return t.executeApprove(ctx, args)
	case "reject":
		return t.executeReject(ctx, args)
	case "search":
		return t.executeSearch(ctx, args)
	case "review":
		return t.executeReview(ctx, args)
	case "comment":
		return t.executeComment(ctx, args)
	case "progress":
		return t.executeProgress(ctx, args)
	case "attach":
		return t.executeAttach(ctx, args)
	case "update":
		return t.executeUpdate(ctx, args)
	case "ask_user":
		return t.executeAskUser(ctx, args)
	case "clear_ask_user":
		return t.executeClearAskUser(ctx, args)
	case "retry":
		return t.executeRetry(ctx, args)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s (use list, get, create, claim, complete, cancel, search, review, comment, progress, attach, update, ask_user, clear_ask_user, or retry)", action))
	}
}

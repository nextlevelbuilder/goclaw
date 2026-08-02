package tools

import (
	"context"
	"fmt"
	"strings"
)

// TeamTasksTool exposes the shared team task list to agents.
// Actions are filtered by TeamActionPolicy (full in standard, limited in lite).
type TeamTasksTool struct {
	manager TeamToolBackend
	policy  TeamActionPolicy
}

func NewTeamTasksTool(manager TeamToolBackend, policy TeamActionPolicy) *TeamTasksTool {
	return &TeamTasksTool{manager: manager, policy: policy}
}

func (t *TeamTasksTool) Name() string { return ToolNameTeamTasks }

func (t *TeamTasksTool) Description() string {
	return "Manage the shared team task list (create, claim, complete, track progress). See TEAM.md for available actions and team context."
}

func (t *TeamTasksTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"enum":        t.policy.AllowedActions(),
				"description": t.buildActionDescription(),
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
				"description": "Task description — ONE specific action with clear objective and expected output. Detailed context is fine, but if you need TWO different skills (research+writing, design+coding), split into separate tasks. Include all context the assignee needs.",
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
				"description": "Comment type for action=comment: 'note' (default, share findings) or 'blocker' (you are BLOCKED and need coordinator input). For a workflow step a blocker marks the step blocked and asks the coordinator to resolve it (retry, cancel, or fail) — it does NOT fail the whole workflow. For a standalone task it escalates to the leader.",
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
			"path": map[string]any{
				"type":        "string",
				"description": "Workspace file path (for attach)",
			},
			"task_type": map[string]any{
				"type":        "string",
				"description": "Task type for create: 'general' (default), 'request', or 'note'",
			},
			"assignee": map[string]any{
				"type":        "string",
				"description": "Agent key to assign task to (REQUIRED for create). Auto-dispatches to that team member.",
			},
			"page": map[string]any{
				"type":        "number",
				"description": "Page number for list/search (default 1, 30 per page)",
			},
			"tasks": map[string]any{
				"type":        "array",
				"description": "Required for create_dag: the WHOLE multi-task graph in one call. Each entry is one node. Use unique local ids; blocked_by lists local ids within this same batch (empty for roots); exactly one entry has terminal=true (the integration task whose result is delivered to the requester).",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":          map[string]any{"type": "string", "description": "Unique local id within this batch (e.g. \"a\", \"b\"). Mapped to a real UUID on persist."},
						"subject":     map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
						"assignee":    map[string]any{"type": "string", "description": "Agent key (member for non-terminal work; lead or member for the terminal integration task)."},
						"blocked_by":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Local ids this task depends on within this batch. Empty array for roots."},
						"reviews":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Optional: local ids of the producer tasks this task independently reviews. Makes this task a critic: its assignee must differ from every reviewed producer's assignee, each reviewed producer must reach it via blocked_by, and it must reach the terminal integration task. The terminal task cannot declare reviews."},
						"terminal":    map[string]any{"type": "boolean", "description": "true for the single integration task whose result is delivered to the requester."},
					},
					"required": []string{"id", "subject", "description", "assignee", "blocked_by", "terminal"},
				},
			},
		},
		"required": []string{"action"},
	}
}

// buildActionDescription returns the action parameter description based on policy.
// Includes per-action param guide so models know which params to send.
func (t *TeamTasksTool) buildActionDescription() string {
	var base strings.Builder
	base.WriteString("Available actions: " + strings.Join(t.policy.AllowedActions(), ", ") + ".")
	if t.policy.IsAllowed("ask_user") {
		base.WriteString(" ask_user: set a periodic reminder. clear_ask_user: cancel reminder.")
	}
	if t.policy.IsAllowed("retry") {
		base.WriteString(" retry: re-dispatch a stale/failed task.")
	}
	// Per-action param guide — only list actions allowed by policy.
	base.WriteString("\n\nParams per action (only send listed params):\n")
	guide := map[string]string{
		"list":            "- list: status?, page?\n",
		"get":             "- get: task_id\n",
		"create":          "- create: subject, description, assignee, priority?, blocked_by?, require_approval?, task_type?\n",
		"claim":           "- claim: task_id\n",
		"complete":        "- complete: task_id?, result\n",
		"cancel":          "- cancel: task_id, text\n",
		"search":          "- search: query, page?\n",
		"review":          "- review: task_id\n",
		"comment":         "- comment: task_id?, text, type?\n",
		"progress":        "- progress: task_id?, percent, text?\n",
		"attach":          "- attach: task_id, path\n",
		"update":          "- update: task_id, subject?, description?, priority?, blocked_by?\n",
		"approve":         "- approve: task_id\n",
		"reject":          "- reject: task_id, text\n",
		"ask_user":        "- ask_user: task_id, text\n",
		"clear_ask_user":  "- clear_ask_user: task_id\n",
		"retry":           "- retry: task_id\n",
		"retry_blocked":   "- retry_blocked: text (revised instruction for the blocked workflow step; the target is resolved from the recovery context)\n",
		"cancel_workflow": "- cancel_workflow: text (reason; the workflow is resolved from the recovery context)\n",
		"fail_workflow":   "- fail_workflow: text (user-facing failure reason; the workflow is resolved from the recovery context)\n",
		"create_dag":      "- create_dag: tasks (array of {id, subject, description, assignee, blocked_by, reviews?, terminal}). Coordinator only. Build the WHOLE graph in this one call: unique local id per task; blocked_by lists local ids within this batch (empty for roots); exactly one task has terminal=true and is the integration task whose result is delivered to the requester. Optional reviews on a task makes it an independent critic of the listed producer ids — required when the request needs independent review before integration.\n",
	}
	for _, action := range t.policy.AllowedActions() {
		if line, ok := guide[action]; ok {
			base.WriteString(line)
		}
	}
	return base.String()
}

func (t *TeamTasksTool) Execute(ctx context.Context, args map[string]any) *Result {
	action, _ := args["action"].(string)

	// Distinguish an action that exists but is edition-gated from a value outside
	// the action contract. This keeps policy denials precise without masking malformed
	// tool calls as edition differences.
	if !actionAllowed(fullActions, action) {
		return ErrorResult(fmt.Sprintf("unknown action: %s", action))
	}
	if !t.policy.IsAllowed(action) {
		return ErrorResult(fmt.Sprintf("action %q is not available in this edition", action))
	}

	// Block mutations during notification runs — leader may only relay status.
	if RunKindFromCtx(ctx) == RunKindNotification {
		switch action {
		case "list", "get", "search":
			// Read-only actions allowed.
		default:
			return ErrorResult("This is a notification run. Your role is to relay task status to the user in a natural, conversational style. Do not modify tasks.")
		}
	}

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
	case "retry_blocked":
		return t.executeRetryBlocked(ctx, args)
	case "cancel_workflow":
		return t.executeCancelWorkflow(ctx, args)
	case "fail_workflow":
		return t.executeFailWorkflow(ctx, args)
	case "create_dag":
		return t.executeCreateDAG(ctx, args)
	default:
		return ErrorResult(fmt.Sprintf("unknown action: %s", action))
	}
}

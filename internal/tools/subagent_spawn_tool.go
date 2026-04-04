package tools

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SpawnTool spawns subagent clones to handle tasks in the background.
//
// Routing:
//   - mode="async" (default): return immediately, subagent announces result when done
//   - mode="sync": block until done, return result inline
type SpawnTool struct {
	subagentMgr *SubagentManager
	parentID    string
	depth       int
}

func NewSpawnTool(manager *SubagentManager, parentID string, depth int) *SpawnTool {
	return &SpawnTool{
		subagentMgr: manager,
		parentID:    parentID,
		depth:       depth,
	}
}

func (t *SpawnTool) Name() string { return "spawn" }

func (t *SpawnTool) Description() string {
	return "Spawn a subagent to handle a task in the background. The subagent runs independently and reports back when done."
}

func (t *SpawnTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{
				"type":        "string",
				"description": "'spawn' (default), 'list', 'cancel', 'steer', or 'wait'",
			},
			"task": map[string]any{
				"type":        "string",
				"description": "The task to complete (required for action=spawn)",
			},
			"mode": map[string]any{
				"type":        "string",
				"description": "'async' (default, returns immediately) or 'sync' (blocks until done)",
			},
			"label": map[string]any{
				"type":        "string",
				"description": "Short label for the task (for display)",
			},
			"model": map[string]any{
				"type":        "string",
				"description": "Optional model override (e.g. 'anthropic/claude-sonnet-4-5-20250929')",
			},
			"id": map[string]any{
				"type":        "string",
				"description": "Task ID for cancel/steer. For cancel: use 'all' to cancel all or 'last' for most recent",
			},
			"message": map[string]any{
				"type":        "string",
				"description": "New instructions (required for action=steer)",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Timeout in seconds for action=wait (default 300)",
			},
		},
		"required": []string{"task"},
	}
}

func (t *SpawnTool) Execute(ctx context.Context, args map[string]any) *Result {
	action, _ := args["action"].(string)
	if action == "" {
		action = "spawn"
	}

	switch action {
	case "list":
		return t.executeList(ctx)
	case "cancel":
		return t.executeCancel(ctx, args)
	case "steer":
		return t.executeSteer(ctx, args)
	case "wait":
		return t.executeWait(ctx, args)
	default:
		return t.executeSpawn(ctx, args)
	}
}

func (t *SpawnTool) executeSpawn(ctx context.Context, args map[string]any) *Result {
	// Reject legacy "agent" parameter — delegation was removed.
	// Guide the LLM to use team_tasks for team coordination.
	if agentKey, _ := args["agent"].(string); agentKey != "" {
		return ErrorResult(fmt.Sprintf(
			"spawn does not accept 'agent' parameter. spawn is for self-clone subagent only. "+
				"To delegate work to team member %q, use: team_tasks(action=\"create\", subject=\"...\", description=\"...\", assignee=%q)",
			agentKey, agentKey))
	}

	// Validate tenant isolation: callers must have a tenant in context.
	// Self-clone subagents inherit caller's context (WithoutCancel), so tenant propagates automatically.
	if store.TenantIDFromContext(ctx) == uuid.Nil {
		return ErrorResult("spawn requires tenant context: no tenant ID found in request context")
	}

	task, _ := args["task"].(string)
	if task == "" {
		return ErrorResult("task parameter is required")
	}

	mode, _ := args["mode"].(string)
	if mode == "sync" {
		return t.executeSubagentSync(ctx, args, task)
	}
	return t.executeSubagentAsync(ctx, args, task)
}

// executeSubagentAsync spawns an async self-clone.
func (t *SpawnTool) executeSubagentAsync(ctx context.Context, args map[string]any, task string) *Result {
	label, _ := args["label"].(string)
	modelOverride, _ := args["model"].(string)

	channel := ToolChannelFromCtx(ctx)
	chatID := ToolChatIDFromCtx(ctx)
	peerKind := ToolPeerKindFromCtx(ctx)
	callback := ToolAsyncCBFromCtx(ctx)

	parentID := ToolAgentKeyFromCtx(ctx)
	if parentID == "" {
		parentID = t.parentID
	}

	msg, err := t.subagentMgr.Spawn(ctx, parentID, t.depth, task, label, modelOverride,
		channel, chatID, peerKind, callback)
	if err != nil {
		return ErrorResult(err.Error())
	}

	forLLM := fmt.Sprintf(`{"status":"accepted","label":%q}
%s
After all spawn tool calls in this turn are complete, briefly tell the user what tasks you've started. Subagents will announce results when done — do NOT wait or poll.`, label, msg)

	return AsyncResult(forLLM)
}

// executeSubagentSync runs a sync self-clone.
func (t *SpawnTool) executeSubagentSync(ctx context.Context, args map[string]any, task string) *Result {
	label, _ := args["label"].(string)
	if label == "" {
		label = truncate(task, 50)
	}

	channel := ToolChannelFromCtx(ctx)
	chatID := ToolChatIDFromCtx(ctx)

	parentID := ToolAgentKeyFromCtx(ctx)
	if parentID == "" {
		parentID = t.parentID
	}

	result, iterations, err := t.subagentMgr.RunSync(ctx, parentID, t.depth, task, label,
		channel, chatID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("Subagent '%s' failed: %v", label, err))
	}

	forUser := fmt.Sprintf("Subagent '%s' completed.", label)
	if len(result) > 500 {
		forUser += "\n" + result[:500] + "..."
	} else {
		forUser += "\n" + result
	}

	forLLM := fmt.Sprintf("Subagent '%s' completed in %d iterations.\n\nFull result:\n%s",
		label, iterations, result)

	return &Result{ForLLM: forLLM, ForUser: forUser}
}

// SetContext is a no-op; channel/chatID are now read from ctx (thread-safe).
func (t *SpawnTool) SetContext(channel, chatID string) {}

// SetPeerKind is a no-op; peerKind is now read from ctx (thread-safe).
func (t *SpawnTool) SetPeerKind(peerKind string) {}

// SetCallback is a no-op; callback is now read from ctx (thread-safe).
func (t *SpawnTool) SetCallback(cb AsyncCallback) {}

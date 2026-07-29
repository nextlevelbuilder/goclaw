package tools

import (
	"context"
	"encoding/json"
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
				"description": "'spawn' (default), 'get', 'list', 'cancel', 'steer', or 'wait'",
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
			"completion_id": map[string]any{
				"type":        "string",
				"description": "Durable completion UUID returned by async spawn (required for action=get)",
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
	}
}

func (t *SpawnTool) Execute(ctx context.Context, args map[string]any) *Result {
	action, _ := args["action"].(string)
	if action == "" {
		action = "spawn"
	}

	switch action {
	case "get":
		return t.executeGet(ctx, args)
	case "list":
		return t.executeList(ctx)
	case "cancel":
		return t.executeCancel(ctx, args)
	case "steer":
		return t.executeSteer(ctx, args)
	case "wait":
		return t.executeWait(ctx, args)
	case "spawn":
		return t.executeSpawn(ctx, args)
	default:
		return ErrorResult(fmt.Sprintf("unknown spawn action %q", action))
	}
}

func (t *SpawnTool) executeGet(ctx context.Context, args map[string]any) *Result {
	rawID, _ := args["completion_id"].(string)
	completionID, err := uuid.Parse(rawID)
	if err != nil {
		return ErrorResult("completion_id must be a valid UUID")
	}
	scope := subagentScopeFromContext(ctx)
	task, err := t.subagentMgr.GetPersistedTask(ctx, scope, completionID)
	if err != nil {
		return ErrorResult(err.Error())
	}
	if task == nil {
		return ErrorResult("subagent completion not found")
	}
	payload, err := json.Marshal(persistedCompletionPayload(task))
	if err != nil {
		return ErrorResult("failed to encode subagent completion")
	}
	return NewResult(string(payload))
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
	if mode == "" {
		mode = "async"
	}
	if err := validateDelegationChildRunMode(ctx, "spawn", mode); err != nil {
		return ErrorResult(err.Error())
	}
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

	receipt, err := t.subagentMgr.SpawnWithReceipt(ctx, parentID, t.depth, task, label, modelOverride,
		channel, chatID, peerKind, callback)
	if err != nil {
		return ErrorResult(err.Error())
	}

	accepted := map[string]any{
		"status":  "accepted",
		"label":   label,
		"task_id": receipt.TaskID,
	}
	if receipt.CompletionID != uuid.Nil {
		accepted["completion_id"] = receipt.CompletionID.String()
	}
	acceptedJSON, _ := json.Marshal(accepted)
	forLLM := fmt.Sprintf(`%s
%s
After all spawn tool calls in this turn are complete, briefly tell the user what tasks you've started. Subagents will announce results when done. If an announcement is missed, retrieve the durable result with spawn(action="get", completion_id="..."). Do NOT wait or poll while the task is running.`, acceptedJSON, receipt.Message)

	return AsyncResult(forLLM)
}

func persistedCompletionPayload(task *store.SubagentTaskData) map[string]any {
	payload := map[string]any{
		"completion_id": task.ID.String(),
		"status":        task.Status,
		"subject":       task.Subject,
		"created_at":    task.CreatedAt,
		"updated_at":    task.UpdatedAt,
	}
	if task.Result != nil {
		payload["result"] = *task.Result
	}
	if task.CompletedAt != nil {
		payload["completed_at"] = *task.CompletedAt
	}
	if task.Metadata != nil {
		if runtimeID, ok := task.Metadata[asyncCompletionRuntimeIDKey].(string); ok && runtimeID != "" {
			payload["task_id"] = runtimeID
		}
		if delivery, ok := task.Metadata[asyncCompletionDeliveryKey].(string); ok && delivery != "" {
			payload[asyncCompletionDeliveryKey] = delivery
		}
		if media := persistedCompletionMediaPayload(task.Metadata[asyncCompletionMediaKey]); len(media) > 0 {
			payload["media"] = media
		}
	}
	return payload
}

// executeSubagentSync runs a sync self-clone.
func (t *SpawnTool) executeSubagentSync(ctx context.Context, args map[string]any, task string) *Result {
	label, _ := args["label"].(string)
	modelOverride, _ := args["model"].(string)
	if label == "" {
		label = truncate(task, 50)
	}

	channel := ToolChannelFromCtx(ctx)
	chatID := ToolChatIDFromCtx(ctx)

	parentID := ToolAgentKeyFromCtx(ctx)
	if parentID == "" {
		parentID = t.parentID
	}

	result, media, iterations, err := t.subagentMgr.RunSync(ctx, parentID, t.depth, task, label, modelOverride,
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

	return &Result{ForLLM: forLLM, ForUser: forUser, Media: media}
}

// SetContext is a no-op; channel/chatID are now read from ctx (thread-safe).
func (t *SpawnTool) SetContext(channel, chatID string) {}

// SetPeerKind is a no-op; peerKind is now read from ctx (thread-safe).
func (t *SpawnTool) SetPeerKind(peerKind string) {}

// SetCallback is a no-op; callback is now read from ctx (thread-safe).
func (t *SpawnTool) SetCallback(cb AsyncCallback) {}

func (t *SpawnTool) scopeFromContext(ctx context.Context) TaskScope {
	scope := subagentScopeFromContext(ctx)
	if scope.RootAgentKey == "" {
		scope.RootAgentKey = t.parentID
	}
	return scope
}

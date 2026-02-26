package tools

import (
	"context"
	"fmt"
)

// SpawnTool is an async tool that spawns a subagent in the background.
// Per-call values (channel, chatID, peerKind, callback) are read from ctx for thread-safety.
type SpawnTool struct {
	manager  *SubagentManager
	parentID string
	depth    int
}

func NewSpawnTool(manager *SubagentManager, parentID string, depth int) *SpawnTool {
	return &SpawnTool{
		manager:  manager,
		parentID: parentID,
		depth:    depth,
	}
}

func (t *SpawnTool) Name() string        { return "spawn" }
func (t *SpawnTool) Description() string {
	return "Spawn a subagent to handle a task in the background. The subagent runs independently and reports back when done. Use for complex or time-consuming tasks."
}

func (t *SpawnTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"task": map[string]interface{}{
				"type":        "string",
				"description": "The task for the subagent to complete",
			},
			"label": map[string]interface{}{
				"type":        "string",
				"description": "Short label for the task (for display)",
			},
			"model": map[string]interface{}{
				"type":        "string",
				"description": "Optional model override for this subagent (e.g. 'anthropic/claude-sonnet-4-5-20250929')",
			},
		},
		"required": []string{"task"},
	}
}

func (t *SpawnTool) Execute(ctx context.Context, args map[string]interface{}) *Result {
	task, _ := args["task"].(string)
	if task == "" {
		return ErrorResult("task parameter is required")
	}
	label, _ := args["label"].(string)
	modelOverride, _ := args["model"].(string)

	// Read per-call values from ctx (thread-safe)
	channel := ToolChannelFromCtx(ctx)
	chatID := ToolChatIDFromCtx(ctx)
	peerKind := ToolPeerKindFromCtx(ctx)
	callback := ToolAsyncCBFromCtx(ctx)

	// Resolve parent agent from ctx (managed mode) with fallback to construction-time default
	parentID := ToolAgentKeyFromCtx(ctx)
	if parentID == "" {
		parentID = t.parentID
	}

	msg, err := t.manager.Spawn(ctx, parentID, t.depth, task, label, modelOverride,
		channel, chatID, peerKind, callback)
	if err != nil {
		return ErrorResult(err.Error())
	}

	// Match TS pattern: return structured status + instruction for the LLM.
	// TS returns {status: "accepted", childSessionKey, runId} and the LLM
	// naturally acknowledges. We add a brief instruction since weaker models
	// may return empty content after multiple spawn calls.
	forLLM := fmt.Sprintf(`{"status":"accepted","label":%q}
%s
After all spawn tool calls in this turn are complete, briefly tell the user what tasks you've started. Subagents will announce results when done — do NOT wait or poll.`, label, msg)

	return AsyncResult(forLLM)
}

// SetContext is a no-op; channel/chatID are now read from ctx (thread-safe).
func (t *SpawnTool) SetContext(channel, chatID string) {}

// SetPeerKind is a no-op; peerKind is now read from ctx (thread-safe).
func (t *SpawnTool) SetPeerKind(peerKind string) {}

// SetCallback is a no-op; callback is now read from ctx (thread-safe).
func (t *SpawnTool) SetCallback(cb AsyncCallback) {}

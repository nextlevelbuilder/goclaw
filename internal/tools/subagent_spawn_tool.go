package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SpawnTool is the unified tool for spawning subagents and delegating to other agents.
// Replaces the old separate spawn, subagent, and delegate tools.
//
// Routing:
//   - No agent param (or agent == self): subagent (clone self)
//   - agent param set to a different agent: delegation (run target agent)
//   - mode="sync": block until done; mode="async" (default): return immediately
type SpawnTool struct {
	subagentMgr *SubagentManager
	delegateMgr *DelegateManager // nil if not configured; injected via SetDelegateManager
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

// SetDelegateManager injects delegation capability.
func (t *SpawnTool) SetDelegateManager(dm *DelegateManager) { t.delegateMgr = dm }

func (t *SpawnTool) Name() string { return "spawn" }

func (t *SpawnTool) Description() string {
	if t.delegateMgr != nil {
		return "Spawn an agent to handle a task. Omit 'agent' to clone yourself, or specify 'agent' to delegate to a specialized agent. See DELEGATION.md for available agents."
	}
	return "Spawn a subagent to handle a task in the background. The subagent runs independently and reports back when done."
}

func (t *SpawnTool) Parameters() map[string]any {
	props := map[string]any{
		"action": map[string]any{
			"type":        "string",
			"description": "'spawn' (default), 'list', 'cancel', or 'steer'",
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
	}

	// Add delegation-specific params when delegate manager is available
	if t.delegateMgr != nil {
		props["agent"] = map[string]any{
			"type":        "string",
			"description": "Target agent key. Omit to clone yourself, specify to delegate to another agent",
		}
		props["context"] = map[string]any{
			"type":        "string",
			"description": "Optional additional context for the target agent (used with agent param)",
		}
		props["team_task_id"] = map[string]any{
			"type":        "string",
			"description": "Omit to auto-create a task (recommended). Only set for dependency chains with blocked_by tasks.",
		}
		props["estimated_duration"] = map[string]any{
			"type":        "number",
			"description": "Estimated task duration in seconds. A progress notification is sent to the user if the task takes longer than this. Default 90.",
		}
	}

	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   []string{"task"},
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
	default:
		return t.executeSpawn(ctx, args)
	}
}

// executeSpawn routes to subagent (self-clone) or delegation (different agent).
func (t *SpawnTool) executeSpawn(ctx context.Context, args map[string]any) *Result {
	task, _ := args["task"].(string)
	if task == "" {
		return ErrorResult("task parameter is required")
	}

	agentKey, _ := args["agent"].(string)
	selfKey := ToolAgentKeyFromCtx(ctx)
	if selfKey == "" {
		selfKey = t.parentID
	}

	// If agent is specified and different from self → delegation
	if agentKey != "" && agentKey != selfKey && t.delegateMgr != nil {
		return t.executeDelegation(ctx, args, agentKey, task)
	}

	// Self-clone path
	mode, _ := args["mode"].(string)
	if mode == "sync" {
		return t.executeSubagentSync(ctx, args, task)
	}
	return t.executeSubagentAsync(ctx, args, task)
}

// executeSubagentAsync spawns an async self-clone (old SpawnTool behavior).
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

// executeSubagentSync runs a sync self-clone (old SubagentTool action=run behavior).
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

// executeDelegation delegates to a different agent (old DelegateTool behavior).
func (t *SpawnTool) executeDelegation(ctx context.Context, args map[string]any, agentKey, task string) *Result {
	extraContext, _ := args["context"].(string)
	mode, _ := args["mode"].(string)
	if mode == "" {
		mode = "async"
	}

	var teamTaskID uuid.UUID
	if ttID, _ := args["team_task_id"].(string); ttID != "" {
		teamTaskID, _ = uuid.Parse(ttID)
	}

	var estimatedDuration time.Duration
	if ed, ok := args["estimated_duration"].(float64); ok && ed > 0 {
		estimatedDuration = time.Duration(ed) * time.Second
	}

	label, _ := args["label"].(string)

	opts := DelegateOpts{
		TargetAgentKey:    agentKey,
		Task:              task,
		Context:           extraContext,
		Mode:              mode,
		TeamTaskID:        teamTaskID,
		EstimatedDuration: estimatedDuration,
		Label:             label,
	}

	if mode == "async" {
		result, err := t.delegateMgr.DelegateAsync(ctx, opts)
		if err != nil {
			return ErrorResult(err.Error())
		}
		forLLM := fmt.Sprintf(`{"status":"accepted","delegation_id":%q,"target":%q,"mode":"async","team_task_id":%q}
Delegated to %q (async, id=%s). The result will be announced automatically when done — do NOT wait or poll.
Briefly tell the user what you've delegated and to whom. Be friendly and natural.`,
			result.DelegationID, agentKey, result.TeamTaskID, agentKey, result.DelegationID)
		return AsyncResult(forLLM)
	}

	// Sync delegation
	result, err := t.delegateMgr.Delegate(ctx, opts)
	if err != nil {
		return ErrorResult(err.Error())
	}

	mediaNote := ""
	if len(result.Media) > 0 {
		mediaNote = fmt.Sprintf("\n\n[%d media file(s) attached — will be delivered automatically. Do NOT recreate or call create_image.]",
			len(result.Media))
	}

	forLLM := fmt.Sprintf(
		"Delegation to %q completed (%d iterations).\n\nResult:\n%s%s\n\n"+
			"Present the information above to the user in YOUR OWN voice and persona. "+
			"Do NOT adopt the delegate agent's personality, tone, or self-references. "+
			"Rephrase and summarize naturally as yourself.",
		agentKey, result.Iterations, result.Content, mediaNote)

	toolResult := NewResult(forLLM)
	if len(result.Media) > 0 {
		toolResult.Media = result.Media
	}
	return toolResult
}

// executeList shows active subagents and delegations.
func (t *SpawnTool) executeList(ctx context.Context) *Result {
	var sections []string

	// Subagent tasks
	parentID := ToolAgentKeyFromCtx(ctx)
	if parentID == "" {
		parentID = t.parentID
	}
	tasks := t.subagentMgr.ListTasks(parentID)
	if len(tasks) > 0 {
		var lines []string
		running, completed, cancelled := 0, 0, 0
		for _, task := range tasks {
			switch task.Status {
			case "running":
				running++
			case "completed":
				completed++
			case "cancelled":
				cancelled++
			}
			line := fmt.Sprintf("- [%s] %s (id=%s, status=%s)", task.Label, truncate(task.Task, 60), task.ID, task.Status)
			if task.CompletedAt > 0 {
				dur := time.Duration(task.CompletedAt-task.CreatedAt) * time.Millisecond
				line += fmt.Sprintf(", took %s", dur.Round(time.Millisecond))
			}
			lines = append(lines, line)
		}
		sections = append(sections, fmt.Sprintf("Subagent tasks: %d running, %d completed, %d cancelled\n%s",
			running, completed, cancelled, strings.Join(lines, "\n")))
	}

	// Delegation tasks
	if t.delegateMgr != nil {
		sourceAgentID := store.AgentIDFromContext(ctx)
		delegations := t.delegateMgr.ListActive(sourceAgentID)
		if len(delegations) > 0 {
			out, _ := json.Marshal(map[string]any{
				"delegations": delegations,
				"count":       len(delegations),
			})
			sections = append(sections, "Delegations:\n"+string(out))
		}
	}

	if len(sections) == 0 {
		return &Result{ForLLM: "No active tasks found."}
	}
	return &Result{ForLLM: strings.Join(sections, "\n\n")}
}

// executeCancel cancels a subagent or delegation by ID.
func (t *SpawnTool) executeCancel(ctx context.Context, args map[string]any) *Result {
	id, _ := args["id"].(string)
	if id == "" {
		return ErrorResult("id is required for action=cancel")
	}

	// Try subagent first
	if t.subagentMgr.CancelTask(id) {
		return &Result{ForLLM: fmt.Sprintf("Task '%s' cancelled.", id)}
	}

	// Try delegation
	if t.delegateMgr != nil && t.delegateMgr.Cancel(id) {
		return NewResult(fmt.Sprintf("Delegation '%s' cancelled.", id))
	}

	return ErrorResult(fmt.Sprintf("Task '%s' not found or not running.", id))
}

// executeSteer redirects a running subagent with new instructions.
func (t *SpawnTool) executeSteer(ctx context.Context, args map[string]any) *Result {
	id, _ := args["id"].(string)
	if id == "" {
		return ErrorResult("id is required for action=steer")
	}
	message, _ := args["message"].(string)
	if message == "" {
		return ErrorResult("message is required for action=steer")
	}

	msg, err := t.subagentMgr.Steer(ctx, id, message, nil)
	if err != nil {
		return ErrorResult(err.Error())
	}
	return &Result{ForLLM: msg}
}

// SetContext is a no-op; channel/chatID are now read from ctx (thread-safe).
func (t *SpawnTool) SetContext(channel, chatID string) {}

// SetPeerKind is a no-op; peerKind is now read from ctx (thread-safe).
func (t *SpawnTool) SetPeerKind(peerKind string) {}

// SetCallback is a no-op; callback is now read from ctx (thread-safe).
func (t *SpawnTool) SetCallback(cb AsyncCallback) {}

// --- Helpers moved from old subagent_tool.go ---

// FilterDenyList returns tool names from the registry excluding denied tools.
func FilterDenyList(reg *Registry, denyList []string) []string {
	deny := make(map[string]bool, len(denyList))
	for _, n := range denyList {
		deny[n] = true
	}

	var allowed []string
	for _, name := range reg.List() {
		if !deny[name] {
			allowed = append(allowed, name)
		}
	}
	return allowed
}

// IsSubagentDenied checks if a tool name is in the subagent deny list.
func IsSubagentDenied(toolName string, depth, maxDepth int) bool {
	for _, d := range SubagentDenyAlways {
		if strings.EqualFold(toolName, d) {
			return true
		}
	}
	if depth >= maxDepth {
		for _, d := range SubagentDenyLeaf {
			if strings.EqualFold(toolName, d) {
				return true
			}
		}
	}
	return false
}

package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// SubagentTool manages subagents: run tasks synchronously, list running tasks, or cancel them.
// Per-call values (channel, chatID, peerKind) are read from ctx for thread-safety.
type SubagentTool struct {
	manager  *SubagentManager
	parentID string
	depth    int
}

func NewSubagentTool(manager *SubagentManager, parentID string, depth int) *SubagentTool {
	return &SubagentTool{
		manager:  manager,
		parentID: parentID,
		depth:    depth,
	}
}

func (t *SubagentTool) Name() string { return "subagent" }
func (t *SubagentTool) Description() string {
	return "Manage subagents: run a task synchronously, list active/completed tasks, cancel a running task, or steer (redirect) a running task with new instructions."
}

func (t *SubagentTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"action": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"run", "list", "cancel", "steer"},
				"description": "Action: 'run' (default) executes a task synchronously, 'list' shows active/completed subagents, 'cancel' stops a running subagent (use subagent_id='all' or 'last' for bulk), 'steer' cancels and restarts a subagent with new instructions",
			},
			"task": map[string]interface{}{
				"type":        "string",
				"description": "The task for the subagent to complete (required for action=run)",
			},
			"label": map[string]interface{}{
				"type":        "string",
				"description": "Short label for the task (for display, used with action=run)",
			},
			"subagent_id": map[string]interface{}{
				"type":        "string",
				"description": "Subagent ID (required for action=cancel/steer). For cancel: use 'all' to cancel all or 'last' for most recent",
			},
			"message": map[string]interface{}{
				"type":        "string",
				"description": "New instructions for the subagent (required for action=steer)",
			},
		},
	}
}

func (t *SubagentTool) Execute(ctx context.Context, args map[string]interface{}) *Result {
	action, _ := args["action"].(string)
	if action == "" {
		action = "run"
	}

	switch action {
	case "list":
		return t.executeList()
	case "cancel":
		return t.executeCancel(args)
	case "steer":
		return t.executeSteer(ctx, args)
	default:
		return t.executeRun(ctx, args)
	}
}

func (t *SubagentTool) executeList() *Result {
	tasks := t.manager.ListTasks(t.parentID)
	if len(tasks) == 0 {
		return &Result{ForLLM: "No subagent tasks found."}
	}

	var lines []string
	running, completed, cancelled := 0, 0, 0
	for _, task := range tasks {
		status := task.Status
		switch status {
		case "running":
			running++
		case "completed":
			completed++
		case "cancelled":
			cancelled++
		}
		line := fmt.Sprintf("- [%s] %s (id=%s, status=%s)", task.Label, truncate(task.Task, 60), task.ID, status)
		if task.CompletedAt > 0 {
			dur := time.Duration(task.CompletedAt-task.CreatedAt) * time.Millisecond
			line += fmt.Sprintf(", took %s", dur.Round(time.Millisecond))
		}
		lines = append(lines, line)
	}

	summary := fmt.Sprintf("Subagent tasks: %d running, %d completed, %d cancelled\n\n%s",
		running, completed, cancelled, strings.Join(lines, "\n"))
	return &Result{ForLLM: summary}
}

func (t *SubagentTool) executeCancel(args map[string]interface{}) *Result {
	id, _ := args["subagent_id"].(string)
	if id == "" {
		return ErrorResult("subagent_id is required for action=cancel")
	}
	if t.manager.CancelTask(id) {
		return &Result{ForLLM: fmt.Sprintf("Subagent '%s' has been cancelled.", id)}
	}
	return ErrorResult(fmt.Sprintf("Subagent '%s' not found or not running.", id))
}

func (t *SubagentTool) executeSteer(ctx context.Context, args map[string]interface{}) *Result {
	id, _ := args["subagent_id"].(string)
	if id == "" {
		return ErrorResult("subagent_id is required for action=steer")
	}
	message, _ := args["message"].(string)
	if message == "" {
		return ErrorResult("message is required for action=steer")
	}

	msg, err := t.manager.Steer(ctx, id, message, nil)
	if err != nil {
		return ErrorResult(err.Error())
	}
	return &Result{ForLLM: msg}
}

func (t *SubagentTool) executeRun(ctx context.Context, args map[string]interface{}) *Result {
	task, _ := args["task"].(string)
	if task == "" {
		return ErrorResult("task parameter is required for action=run")
	}
	label, _ := args["label"].(string)
	if label == "" {
		label = truncate(task, 50)
	}

	// Read per-call values from ctx (thread-safe)
	channel := ToolChannelFromCtx(ctx)
	chatID := ToolChatIDFromCtx(ctx)

	// Resolve parent agent from ctx (managed mode) with fallback to construction-time default
	parentID := ToolAgentKeyFromCtx(ctx)
	if parentID == "" {
		parentID = t.parentID
	}

	result, iterations, err := t.manager.RunSync(ctx, parentID, t.depth, task, label,
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
func (t *SubagentTool) SetContext(channel, chatID string) {}

// SetPeerKind is a no-op; peerKind is now read from ctx (thread-safe).
func (t *SubagentTool) SetPeerKind(peerKind string) {}

// --- Helper: filter tools by name ---

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

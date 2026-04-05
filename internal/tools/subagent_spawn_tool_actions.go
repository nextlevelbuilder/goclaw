package tools

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// executeList shows active subagent tasks.
func (t *SpawnTool) executeList(ctx context.Context) *Result {
	parentID := ToolAgentKeyFromCtx(ctx)
	if parentID == "" {
		parentID = t.parentID
	}
	tasks := t.subagentMgr.ListTasks(parentID)
	if len(tasks) == 0 {
		return &Result{ForLLM: "No active tasks found."}
	}

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

	return &Result{ForLLM: fmt.Sprintf("Subagent tasks: %d running, %d completed, %d cancelled\n%s",
		running, completed, cancelled, strings.Join(lines, "\n"))}
}

// executeCancel cancels a subagent task by ID.
func (t *SpawnTool) executeCancel(ctx context.Context, args map[string]any) *Result {
	id, _ := args["id"].(string)
	if id == "" {
		return ErrorResult("id is required for action=cancel")
	}

	if t.subagentMgr.CancelTask(id) {
		return &Result{ForLLM: fmt.Sprintf("Task '%s' cancelled.", id)}
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

// executeWait blocks until all children of the calling agent complete or timeout.
func (t *SpawnTool) executeWait(ctx context.Context, args map[string]any) *Result {
	parentID := ToolAgentKeyFromCtx(ctx)
	if parentID == "" {
		parentID = t.parentID
	}

	timeout := 300
	if v, ok := args["timeout"].(float64); ok && v > 0 {
		timeout = int(v)
	}

	tasks, err := t.subagentMgr.WaitForChildren(ctx, parentID, timeout)
	return t.formatWaitResult(tasks, err)
}

func (t *SpawnTool) formatWaitResult(tasks []*SubagentTask, waitErr error) *Result {
	if len(tasks) == 0 {
		msg := "No subagent tasks found."
		if waitErr != nil {
			msg += " Error: " + waitErr.Error()
		}
		return &Result{ForLLM: msg}
	}

	var sb strings.Builder
	completed, failed := 0, 0
	var totalIn, totalOut int64
	for _, task := range tasks {
		switch task.Status {
		case TaskStatusCompleted:
			completed++
		case TaskStatusFailed:
			failed++
		}
		totalIn += task.TotalInputTokens
		totalOut += task.TotalOutputTokens
		sb.WriteString(fmt.Sprintf("- [%s] %s: %s\n", task.Status, task.Label, truncate(task.Result, 200)))
	}

	header := fmt.Sprintf("All %d subagent tasks finished (%d completed, %d failed). Total tokens: %d in / %d out\n\n",
		len(tasks), completed, failed, totalIn, totalOut)
	if waitErr != nil {
		header = fmt.Sprintf("Wait ended: %s. %d tasks total.\n\n", waitErr.Error(), len(tasks))
	}

	return &Result{ForLLM: header + sb.String()}
}

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

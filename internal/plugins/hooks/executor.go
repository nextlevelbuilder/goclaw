// Package hooks — Hook command executor (CP-08).
// Executes shell commands in response to lifecycle events.
package hooks

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// HookResult is the outcome of executing a hook command.
type HookResult struct {
	Event   Event
	Action  string // "allow", "deny", "prevent", "" (no action)
	Message string // output from command, injected into conversation if non-empty
	Error   error
}

// HookDef matches the Hook type from the plugin manifest.
type HookDef struct {
	MatchTool string
	Command   string
	Timeout   time.Duration
}

// HookProvider supplies hooks for a given event.
type HookProvider interface {
	HooksFor(event string) []HookDef
}

// Executor runs hook commands when lifecycle events fire.
type Executor struct {
	provider HookProvider
}

// NewExecutor creates a hook executor.
func NewExecutor(provider HookProvider) *Executor {
	return &Executor{provider: provider}
}

// Fire executes all hooks matching the event.
// The data map provides variables for command expansion.
//
// If any hook returns "prevent", subsequent hooks and the triggering action are cancelled.
// If any hook returns "deny", the triggering action is cancelled but other hooks still run.
func (e *Executor) Fire(ctx context.Context, event Event, data map[string]any) []HookResult {
	if e.provider == nil {
		return nil
	}

	hooks := e.provider.HooksFor(string(event))
	if len(hooks) == 0 {
		return nil
	}

	results := make([]HookResult, 0, len(hooks))

	for _, hook := range hooks {
		// Check match_tool filter
		if hook.MatchTool != "" {
			toolName, _ := data["tool_name"].(string)
			if toolName != hook.MatchTool {
				continue
			}
		}

		// Expand variables in command
		cmd := expandVariables(hook.Command, data)

		// Execute with timeout
		timeout := hook.Timeout
		if timeout == 0 {
			timeout = 10 * time.Second
		}

		result := executeHookCommand(ctx, cmd, timeout, event)
		results = append(results, result)

		// "prevent" stops further hooks AND the triggering action
		if result.Action == "prevent" {
			slog.Warn("hook prevented action",
				"event", event,
				"command", cmd,
				"message", result.Message)
			break
		}
	}

	return results
}

// HasPreventOrDeny checks if any result blocks the triggering action.
func HasPreventOrDeny(results []HookResult) bool {
	for _, r := range results {
		if r.Action == "prevent" || r.Action == "deny" {
			return true
		}
	}
	return false
}

// CollectMessages aggregates non-empty messages from hook results.
func CollectMessages(results []HookResult) string {
	var msgs []string
	for _, r := range results {
		if r.Message != "" {
			msgs = append(msgs, r.Message)
		}
	}
	return strings.Join(msgs, "\n")
}

func executeHookCommand(ctx context.Context, command string, timeout time.Duration, event Event) HookResult {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	out, err := cmd.CombinedOutput()

	result := HookResult{Event: event}

	if err != nil {
		result.Error = err
		result.Action = "" // errors don't block by default
		result.Message = fmt.Sprintf("[hook error: %v] %s", err, strings.TrimSpace(string(out)))
		slog.Debug("hook command failed",
			"event", event,
			"command", command,
			"err", err)
		return result
	}

	output := strings.TrimSpace(string(out))
	result.Message = output

	// Parse action from output (first line)
	if strings.HasPrefix(output, "PREVENT:") {
		result.Action = "prevent"
		result.Message = strings.TrimPrefix(output, "PREVENT:")
	} else if strings.HasPrefix(output, "DENY:") {
		result.Action = "deny"
		result.Message = strings.TrimPrefix(output, "DENY:")
	}

	return result
}

func expandVariables(cmd string, data map[string]any) string {
	for key, val := range data {
		placeholder := "${" + key + "}"
		cmd = strings.ReplaceAll(cmd, placeholder, fmt.Sprintf("%v", val))
	}
	return cmd
}

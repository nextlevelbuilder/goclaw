// Package handlers provides hook handler implementations for the agent hook system.
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

// CommandHandler executes a local shell command with event data on stdin.
// stdout is parsed as a JSON decision payload; stderr is captured for audit.
// Exit 2 → block; exit 0 → allow; other → error.
// Command handler is only permitted on the Lite edition (C2 drop).
type CommandHandler struct {
	Edition edition.Edition
}

// Execute implements hooks.Handler.
func (h *CommandHandler) Execute(ctx context.Context, cfg hooks.HookConfig, ev hooks.Event) (hooks.Decision, error) {
	// Defense-in-depth: re-check edition at dispatch even if edition gate
	// already filtered at config validation time.
	if ok, reason := (hooks.HookEditionPolicy{}).Allow(hooks.HandlerCommand, cfg.Scope, h.Edition); !ok {
		return hooks.DecisionError, fmt.Errorf("hook: command handler not allowed: %s", reason)
	}

	// Command and allowed env vars are stored in cfg.Config map.
	cmd, _ := cfg.Config["command"].(string)
	if cmd == "" {
		return hooks.DecisionError, fmt.Errorf("hook: command handler: missing 'command' in config")
	}

	var allowedVars []string
	if raw, ok := cfg.Config["allowed_env_vars"]; ok {
		if arr, ok := raw.([]any); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					allowedVars = append(allowedVars, s)
				}
			}
		}
	}

	// Serialize event to JSON for stdin.
	eventJSON, err := json.Marshal(ev)
	if err != nil {
		return hooks.DecisionError, fmt.Errorf("hook: command handler: marshal event: %w", err)
	}

	sh, err := findShell()
	if err != nil {
		return hooks.DecisionError, fmt.Errorf("hook: command handler: %w", err)
	}

	//nolint:gosec // Command comes from admin-configured hooks stored in DB, not user input.
	c := exec.CommandContext(ctx, sh, "-c", cmd)
	c.Stdin = bytes.NewReader(eventJSON)
	c.Env = buildAllowedEnv(allowedVars)

	var stderr bytes.Buffer
	c.Stderr = &stderr

	out, execErr := c.Output()
	if execErr != nil {
		if exitErr, ok := execErr.(*exec.ExitError); ok {
			switch exitErr.ExitCode() {
			case 2:
				return hooks.DecisionBlock, nil
			default:
				return hooks.DecisionError, fmt.Errorf("hook: command exited %d: %s", exitErr.ExitCode(), stderr.String())
			}
		}
		return hooks.DecisionError, fmt.Errorf("hook: command exec: %w", execErr)
	}

	// Exit 0: parse stdout as JSON decision payload.
	if len(out) == 0 {
		return hooks.DecisionAllow, nil
	}
	var resp struct {
		Continue *bool `json:"continue"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		// Non-JSON stdout on exit 0 is treated as allow.
		return hooks.DecisionAllow, nil
	}
	if resp.Continue != nil && !*resp.Continue {
		return hooks.DecisionBlock, nil
	}
	return hooks.DecisionAllow, nil
}

// findShell returns the path to sh, falling back to /bin/sh.
func findShell() (string, error) {
	if p, err := exec.LookPath("sh"); err == nil {
		return p, nil
	}
	return "/bin/sh", nil
}

// buildAllowedEnv constructs an env slice containing only the listed keys from
// the current process environment. If allowedVars is empty, env is isolated
// (security-safe default: hooks cannot exfiltrate process secrets).
func buildAllowedEnv(allowedVars []string) []string {
	if len(allowedVars) == 0 {
		return nil
	}
	env := make([]string, 0, len(allowedVars))
	for _, key := range allowedVars {
		if val, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+val)
		}
	}
	return env
}

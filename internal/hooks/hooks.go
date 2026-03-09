// Package hooks provides a general-purpose quality gate / hook evaluation system.
// It supports multiple hook types (command, agent) and can be extended with new evaluators.
package hooks

import "context"

// HookType defines how a hook validates output.
type HookType string

const (
	HookTypeCommand HookType = "command" // shell command; exit 0 = pass
	HookTypeAgent   HookType = "agent"   // delegate to reviewer agent; "approved" = pass
)

// HookConfig defines a single quality gate.
type HookConfig struct {
	Event          string   `json:"event"`                     // e.g. "delegation.completed"
	Type           HookType `json:"type"`                      // "command" or "agent"
	Command        string   `json:"command,omitempty"`         // for type=command: shell command to run
	Agent          string   `json:"agent,omitempty"`           // for type=agent: reviewer agent key
	BlockOnFailure bool     `json:"block_on_failure"`          // true = block and optionally retry
	MaxRetries     int      `json:"max_retries,omitempty"`     // 0 = no retry (only applies when block_on_failure=true)
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"` // per-hook timeout (default 60)
}

// HookContext provides information about what triggered the hook.
type HookContext struct {
	Event          string
	SourceAgentKey string
	TargetAgentKey string
	UserID         string
	Content        string            // the output being validated
	Task           string            // the original task
	Metadata       map[string]string // extra context
}

// HookResult is the outcome of evaluating a hook.
type HookResult struct {
	Passed   bool   // true = output accepted
	Feedback string // on failure: why it failed (used for retry prompt)
}

// HookEvaluator evaluates a single hook against a context.
type HookEvaluator interface {
	Evaluate(ctx context.Context, hook HookConfig, hctx HookContext) (*HookResult, error)
}

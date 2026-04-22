// Package hooks defines the agent hook system: typed events, config structs,
// execution payloads, and the store interface. Handlers and dispatcher live
// in separate files; this file is pure type definitions.
package hooks

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ─── Enum types ─────────────────────────────────────────────────────────────

// HookEvent identifies which lifecycle point a hook fires on.
// Stored as VARCHAR(32) in DB; JSON-encoded as plain string.
type HookEvent string

const (
	// EventSessionStart fires when a new session is established.
	EventSessionStart HookEvent = "session_start"
	// EventUserPromptSubmit fires before the user's message enters the pipeline. BLOCKING.
	EventUserPromptSubmit HookEvent = "user_prompt_submit"
	// EventPreToolUse fires before any tool call executes. BLOCKING.
	EventPreToolUse HookEvent = "pre_tool_use"
	// EventPostToolUse fires after a tool call completes. Non-blocking.
	EventPostToolUse HookEvent = "post_tool_use"
	// EventStop fires when the agent session terminates normally.
	EventStop HookEvent = "stop"
	// EventSubagentStart fires when a sub-agent is spawned. BLOCKING.
	EventSubagentStart HookEvent = "subagent_start"
	// EventSubagentStop fires when a sub-agent finishes.
	EventSubagentStop HookEvent = "subagent_stop"
)

// IsBlocking returns true when the event requires a synchronous allow/block
// decision before the pipeline continues. Fail-closed: blocking events that
// timeout yield Decision=block.
func (e HookEvent) IsBlocking() bool {
	switch e {
	case EventUserPromptSubmit, EventPreToolUse, EventSubagentStart:
		return true
	default:
		return false
	}
}

// MarshalJSON encodes HookEvent as a JSON string (not integer).
func (e HookEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(e))
}

// UnmarshalJSON decodes a JSON string into HookEvent.
func (e *HookEvent) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*e = HookEvent(s)
	return nil
}

// HandlerType identifies the execution mechanism for a hook.
type HandlerType string

const (
	// HandlerCommand runs a local shell command with event data on stdin.
	HandlerCommand HandlerType = "command"
	// HandlerHTTP posts event data to an HTTP endpoint.
	HandlerHTTP HandlerType = "http"
	// HandlerPrompt routes the event through an LLM prompt.
	HandlerPrompt HandlerType = "prompt"
	// HandlerScript runs a user-provided ES5.1 JavaScript snippet in a sandboxed goja runtime.
	HandlerScript HandlerType = "script"
)

// MarshalJSON encodes HandlerType as a JSON string.
func (h HandlerType) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(h))
}

// UnmarshalJSON decodes a JSON string into HandlerType.
func (h *HandlerType) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*h = HandlerType(s)
	return nil
}

// Scope controls which hooks are visible in a resolution pass.
type Scope string

const (
	// ScopeGlobal hooks apply to all tenants. Stored with sentinel UUID as tenant_id.
	ScopeGlobal Scope = "global"
	// ScopeTenant hooks are scoped to a specific tenant.
	ScopeTenant Scope = "tenant"
	// ScopeAgent hooks are scoped to a specific agent within a tenant.
	ScopeAgent Scope = "agent"
)

// SentinelTenantID is the tenant_id value used for global-scope hooks.
// Reuses store.MasterTenantID rather than introducing a separate all-zero
// sentinel: the codebase already treats MasterTenantID as the "no specific
// tenant" ID (see store.IsMasterScope), and there is a seeded tenants row
// with this UUID, so the FK constraint is always satisfied.
var SentinelTenantID = store.MasterTenantID

// Decision is the outcome returned by a hook execution.
type Decision string

const (
	// DecisionAllow permits the operation to proceed.
	DecisionAllow Decision = "allow"
	// DecisionBlock halts the operation. Pipeline stops; fail-closed.
	DecisionBlock Decision = "block"
	// DecisionError indicates the hook encountered an unexpected error.
	DecisionError Decision = "error"
	// DecisionTimeout indicates the hook did not respond within the time budget.
	DecisionTimeout Decision = "timeout"
	// DecisionAsk requests human approval before proceeding. Wave 1: treated as block + warn.
	DecisionAsk Decision = "ask"
	// DecisionDefer defers the decision to an external system. Wave 1: treated as block + warn.
	DecisionDefer Decision = "defer"
)

// IsBlock returns true only when the decision is DecisionBlock.
// Used by the dispatcher sync chain: first block wins.
func (d Decision) IsBlock() bool {
	return d == DecisionBlock
}

// FireResult is the return value of Dispatcher.Fire. Callers read Decision to
// branch on allow/block and apply Updated* when a builtin hook mutated input.
//
// UpdatedToolInput is non-nil only when at least one builtin-source hook in
// the chain returned updatedInput AND the dispatcher applied allow-listed
// fields. Callers overwrite their own tc.Arguments / state.Input with it.
//
// UpdatedRawInput points to a string only when a builtin hook mutated
// rawInput. Callers replace state.Input.Message with the dereferenced value.
//
// For non-builtin scripts returning updatedInput the dispatcher strips the
// mutation + logs a WARN; Updated* stay nil (defense-in-depth against a
// tenant-authored script escalating its capability tier).
type FireResult struct {
	Decision         Decision
	UpdatedToolInput map[string]any
	UpdatedRawInput  *string
}

// ─── Config & execution structs ──────────────────────────────────────────────

// HookConfig mirrors the agent_hooks DB row. All pointer fields correspond to
// nullable columns.
type HookConfig struct {
	ID          uuid.UUID          `json:"id"`
	TenantID    uuid.UUID          `json:"tenant_id"`
	AgentID     *uuid.UUID         `json:"agent_id,omitempty"`     // DEPRECATED: kept for JSON backward compat
	AgentIDs    []uuid.UUID        `json:"agent_ids,omitempty"`
	Event       HookEvent          `json:"event"`
	HandlerType HandlerType        `json:"handler_type"`
	Scope       Scope              `json:"scope"`
	Name        string             `json:"name,omitempty"`
	// Config holds handler-specific options (command path, HTTP URL, prompt template).
	Config      map[string]any     `json:"config"`
	Matcher     string             `json:"matcher,omitempty"`
	IfExpr      string             `json:"if_expr,omitempty"`
	TimeoutMS   int                `json:"timeout_ms"`
	OnTimeout   Decision           `json:"on_timeout"`
	Priority    int                `json:"priority"`
	Enabled     bool               `json:"enabled"`
	Version     int                `json:"version"`
	Source      string             `json:"source"`
	Metadata    map[string]any     `json:"metadata"`
	CreatedBy   *uuid.UUID         `json:"created_by,omitempty"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

// HookExecution mirrors the hook_executions DB row.
// error_detail (BYTEA) is AES-256-GCM encrypted before storage.
type HookExecution struct {
	ID          uuid.UUID  `json:"id"`
	HookID      *uuid.UUID `json:"hook_id,omitempty"` // NULL when hook deleted (ON DELETE SET NULL)
	SessionID   string     `json:"session_id"`
	Event       HookEvent  `json:"event"`
	InputHash   string     `json:"input_hash"`  // canonical-JSON sha256, 64 hex chars
	Decision    Decision   `json:"decision"`
	DurationMS  int        `json:"duration_ms"`
	Retry       int        `json:"retry"`
	DedupKey    string     `json:"dedup_key"`   // (hook_id, event_id) composite
	Error       string     `json:"error"`        // truncated to 256 chars
	ErrorDetail []byte     `json:"error_detail"` // encrypted; nil if no error
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Event is the payload passed to the dispatcher and stored for audit.
// All fields are read-only once constructed; mutation is not safe.
type Event struct {
	// EventID is a unique identifier for this specific event occurrence.
	// Used as part of the dedup key in hook_executions.
	EventID   string
	SessionID string
	TenantID  uuid.UUID
	AgentID   uuid.UUID
	// ToolName is populated for PreToolUse/PostToolUse events.
	ToolName  string
	// ToolInput is the raw tool arguments map for CEL evaluation.
	ToolInput map[string]any
	// RawInput is the user's raw message text (for UserPromptSubmit).
	RawInput  string
	// Depth tracks sub-agent nesting level; max 3 before loop rejection.
	Depth     int
	// HookEvent is the lifecycle event type.
	HookEvent HookEvent
}

package hooks

import (
	"fmt"

	"github.com/dop251/goja"
	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
)

// Defaults and limits for HookConfig fields. Exported so the UI layer can
// surface the same values that Validate() enforces.
const (
	// DefaultTimeoutMS applies when TimeoutMS is zero. Chosen at 5s to match
	// the blocking-sync budget in the plan.
	DefaultTimeoutMS = 5000
	// MaxTimeoutMS caps the per-hook timeout. The 10s ceiling matches the
	// chain wall-time budget; setting above this would let one hook starve
	// the whole chain.
	MaxTimeoutMS = 10_000
	// DefaultSource is the fallback when Source is empty (M1: distinguishes
	// UI-created hooks from agent-seeded ones).
	DefaultSource = "ui"
)

// knownEvents enumerates every HookEvent value that Validate accepts.
// Extending the hook system requires adding the new event both here and in
// types.go's const block.
var knownEvents = map[HookEvent]struct{}{
	EventSessionStart:     {},
	EventUserPromptSubmit: {},
	EventPreToolUse:       {},
	EventPostToolUse:      {},
	EventStop:             {},
	EventSubagentStart:    {},
	EventSubagentStop:     {},
}

// Validate checks a HookConfig for semantic correctness and fills in defaults.
// It MUST be called before Create/Update so that malformed configs never reach
// the DB or the dispatcher. Side effects (on success): TimeoutMS, OnTimeout,
// Version, and Source may be populated with their defaults.
//
// Validation order matters — cheap checks run first so we fail fast:
//  1. Event enum — zero-cost map lookup.
//  2. Scope / tenant / agent invariants — no I/O.
//  3. Edition gate — pure-function policy.
//  4. Matcher + CEL compile — most expensive; done last.
func (h *HookConfig) Validate(ed edition.Edition) error {
	if _, ok := knownEvents[h.Event]; !ok {
		return fmt.Errorf("hook: unknown event %q", h.Event)
	}

	if err := h.validateScope(); err != nil {
		return err
	}

	if err := h.validateHandler(ed); err != nil {
		return err
	}

	if err := h.validateTimeout(); err != nil {
		return err
	}

	h.applyDefaults()

	// Matcher + CEL go last: parsing is the most expensive step and either
	// value is optional (nil pattern / empty expr match all).
	if _, err := CompileMatcher(h.Matcher); err != nil {
		return fmt.Errorf("hook: matcher invalid: %w", err)
	}
	if _, err := CompileCELExpr(h.IfExpr); err != nil {
		return fmt.Errorf("hook: if_expr invalid: %w", err)
	}

	return nil
}

// validateScope enforces (scope, tenant_id, agent_id) invariants:
//   - global → tenant_id MUST be the sentinel UUID.
//   - tenant → tenant_id MUST be non-sentinel.
//   - agent  → tenant_id MUST be non-sentinel AND agent_id non-nil.
func (h *HookConfig) validateScope() error {
	switch h.Scope {
	case ScopeGlobal:
		if h.TenantID != SentinelTenantID {
			return fmt.Errorf("hook: global scope requires sentinel tenant_id, got %s", h.TenantID)
		}
	case ScopeTenant:
		if h.TenantID == SentinelTenantID || h.TenantID == uuid.Nil {
			return fmt.Errorf("hook: tenant scope requires a real tenant_id")
		}
	case ScopeAgent:
		if h.TenantID == SentinelTenantID || h.TenantID == uuid.Nil {
			return fmt.Errorf("hook: agent scope requires a real tenant_id")
		}
		if len(h.AgentIDs) == 0 {
			if h.AgentID != nil && *h.AgentID != uuid.Nil {
				h.AgentIDs = []uuid.UUID{*h.AgentID}
			} else {
				return fmt.Errorf("hook: agent scope requires at least one agent_id")
			}
		}
	default:
		return fmt.Errorf("hook: unknown scope %q", h.Scope)
	}
	return nil
}

// validateHandler enforces per-handler rules + edition policy.
// Command on Standard (any non-global scope) is blocked. Prompt requires at
// least one filter (matcher OR if_expr) so we never fire an LLM per event.
func (h *HookConfig) validateHandler(ed edition.Edition) error {
	// Edition gate — applies the same rule as the dispatcher fire-time gate.
	if ok, reason := (HookEditionPolicy{}).Allow(h.HandlerType, h.Scope, ed); !ok {
		return fmt.Errorf("hook: %s handler not allowed: %s", h.HandlerType, reason)
	}

	switch h.HandlerType {
	case HandlerPrompt:
		if h.Matcher == "" && h.IfExpr == "" {
			return fmt.Errorf("hook: prompt handler requires a matcher or if_expr (runaway-cost guard)")
		}
		if tmpl, _ := h.Config["prompt_template"].(string); tmpl == "" {
			return fmt.Errorf("hook: prompt handler requires non-empty prompt_template")
		}
	case HandlerScript:
		source, _ := h.Config["source"].(string)
		if source == "" {
			return fmt.Errorf("hook: script handler requires non-empty config.source")
		}
		if len(source) > MaxScriptSourceBytes {
			return fmt.Errorf("hook: script source exceeds %d bytes (got %d)", MaxScriptSourceBytes, len(source))
		}
		// Strict=true parallels the handler's compile; surfaces `Line N:M`
		// errors that the UI editor can display inline.
		if _, err := goja.Compile(h.ID.String(), source, true); err != nil {
			return fmt.Errorf("hook: script compile error: %w", err)
		}
	case HandlerCommand, HandlerHTTP:
		// No extra filter required — matcher/if_expr are optional.
	default:
		return fmt.Errorf("hook: unknown handler_type %q", h.HandlerType)
	}
	return nil
}

// MaxScriptSourceBytes mirrors handlers.MaxScriptSourceBytes (32 KiB). Declared
// here to avoid importing the handlers package (which imports goja) just for
// this constant — the size cap is part of the config contract and should be
// enforceable without the runtime dep elsewhere.
const MaxScriptSourceBytes = 32 * 1024

// validateTimeout bounds the per-hook timeout within [0, MaxTimeoutMS] and
// rejects on_timeout values reserved for future phases.
// Zero is normalized to DefaultTimeoutMS in applyDefaults.
func (h *HookConfig) validateTimeout() error {
	if h.TimeoutMS < 0 {
		return fmt.Errorf("hook: timeout_ms must be >= 0, got %d", h.TimeoutMS)
	}
	if h.TimeoutMS > MaxTimeoutMS {
		return fmt.Errorf("hook: timeout_ms %d exceeds max %d", h.TimeoutMS, MaxTimeoutMS)
	}
	// ask/defer are reserved for Wave 3 human-approval + external-arbitration
	// flows. Until the dispatcher wires a `request_user_input` path, accepting
	// these as timeout-fallback would let a misconfigured hook hang blocking
	// events indefinitely.
	if h.OnTimeout == DecisionAsk || h.OnTimeout == DecisionDefer {
		return fmt.Errorf("hook: on_timeout=%q not supported in Wave 1 (reserved for future)", h.OnTimeout)
	}
	return nil
}

// applyDefaults fills optional fields with their plan-specified defaults.
// Called after validation so we never paper over a real error.
func (h *HookConfig) applyDefaults() {
	if h.TimeoutMS == 0 {
		h.TimeoutMS = DefaultTimeoutMS
	}
	if h.OnTimeout == "" {
		if h.Event.IsBlocking() {
			h.OnTimeout = DecisionBlock
		} else {
			h.OnTimeout = DecisionAllow
		}
	}
	if h.Version == 0 {
		h.Version = 1
	}
	if h.Source == "" {
		h.Source = DefaultSource
	}
}

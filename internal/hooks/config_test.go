package hooks_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

// baseValidCommandHook returns a HookConfig that passes Validate on the Lite
// edition. Tests override fields to trigger specific failure modes.
func baseValidCommandHook() hooks.HookConfig {
	return hooks.HookConfig{
		ID:          uuid.New(),
		TenantID:    hooks.SentinelTenantID,
		Event:       hooks.EventPreToolUse,
		HandlerType: hooks.HandlerCommand,
		Scope:       hooks.ScopeGlobal,
		Config:      map[string]any{"command": "/bin/true"},
		Matcher:     "^Write$",
		IfExpr:      `tool_name == "Write"`,
		TimeoutMS:   5000,
		OnTimeout:   hooks.DecisionBlock,
		Priority:    100,
		Enabled:     true,
		Version:     1,
		Source:      "ui",
	}
}

func TestValidate_AcceptsValidCommandHook(t *testing.T) {
	h := baseValidCommandHook()
	if err := h.Validate(edition.Lite); err != nil {
		t.Fatalf("expected nil error; got %v", err)
	}
}

func TestValidate_AcceptsValidHTTPHook(t *testing.T) {
	h := baseValidCommandHook()
	h.HandlerType = hooks.HandlerHTTP
	h.Config = map[string]any{"url": "https://example.com/hook"}
	h.Matcher = "" // http hook without matcher is allowed (matches all tools)
	h.IfExpr = ""
	if err := h.Validate(edition.Standard); err != nil {
		t.Fatalf("expected nil error; got %v", err)
	}
}

func TestValidate_AcceptsValidPromptHook(t *testing.T) {
	h := baseValidCommandHook()
	h.HandlerType = hooks.HandlerPrompt
	h.Config = map[string]any{"prompt_template": "Review this action"}
	h.Matcher = "^Write$"
	h.IfExpr = ""
	if err := h.Validate(edition.Standard); err != nil {
		t.Fatalf("expected nil error; got %v", err)
	}
}

// Prompt handler requires at least ONE filter (matcher OR if_expr) to avoid
// firing the LLM on every event — runaway-cost mitigation (L-risk in plan).
func TestValidate_RejectsPromptWithoutFilter(t *testing.T) {
	h := baseValidCommandHook()
	h.HandlerType = hooks.HandlerPrompt
	h.Config = map[string]any{"template": "x"}
	h.Matcher = ""
	h.IfExpr = ""
	err := h.Validate(edition.Standard)
	if err == nil {
		t.Fatal("expected validation error for prompt without matcher/if_expr")
	}
	if !strings.Contains(err.Error(), "prompt") || !strings.Contains(strings.ToLower(err.Error()), "matcher") {
		t.Errorf("error should mention prompt+matcher; got %q", err)
	}
}

func TestValidate_RejectsInvalidRegex(t *testing.T) {
	h := baseValidCommandHook()
	h.Matcher = "[unclosed"
	err := h.Validate(edition.Lite)
	if err == nil {
		t.Fatal("expected error for malformed regex")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "matcher") {
		t.Errorf("error should mention matcher; got %q", err)
	}
}

func TestValidate_RejectsMalformedCEL(t *testing.T) {
	h := baseValidCommandHook()
	h.IfExpr = `tool_name ===== "bad"` // CEL syntax error
	err := h.Validate(edition.Lite)
	if err == nil {
		t.Fatal("expected error for malformed CEL")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "if_expr") && !strings.Contains(strings.ToLower(err.Error()), "cel") {
		t.Errorf("error should mention if_expr/cel; got %q", err)
	}
}

// Command on Standard + tenant scope is blocked via edition gate. Plan C2 drop:
// command handler is Lite-only (multi-tenant host can't run arbitrary shell).
func TestValidate_RejectsCommandOnStandardTenant(t *testing.T) {
	h := baseValidCommandHook()
	h.Scope = hooks.ScopeTenant
	h.TenantID = uuid.New()
	err := h.Validate(edition.Standard)
	if err == nil {
		t.Fatal("expected error for command on Standard+tenant")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "command") {
		t.Errorf("error should mention command; got %q", err)
	}
}

func TestValidate_RejectsCommandOnStandardAgent(t *testing.T) {
	h := baseValidCommandHook()
	h.Scope = hooks.ScopeAgent
	h.TenantID = uuid.New()
	agentID := uuid.New()
	h.AgentID = &agentID
	err := h.Validate(edition.Standard)
	if err == nil {
		t.Fatal("expected error for command on Standard+agent")
	}
}

func TestValidate_AcceptsCommandOnLiteAnyScope(t *testing.T) {
	for _, scope := range []hooks.Scope{hooks.ScopeGlobal, hooks.ScopeTenant, hooks.ScopeAgent} {
		h := baseValidCommandHook()
		h.Scope = scope
		if scope != hooks.ScopeGlobal {
			h.TenantID = uuid.New()
		}
		if scope == hooks.ScopeAgent {
			agentID := uuid.New()
			h.AgentID = &agentID
		}
		if err := h.Validate(edition.Lite); err != nil {
			t.Errorf("Lite+command+%s should be valid; got %v", scope, err)
		}
	}
}

// TimeoutMS = 0 is replaced with a sane default so callers don't accidentally
// ship zero-timeout hooks that return immediately.
func TestValidate_AppliesTimeoutDefault(t *testing.T) {
	h := baseValidCommandHook()
	h.TimeoutMS = 0
	if err := h.Validate(edition.Lite); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if h.TimeoutMS != hooks.DefaultTimeoutMS {
		t.Errorf("TimeoutMS default = %d, want %d", h.TimeoutMS, hooks.DefaultTimeoutMS)
	}
}

// OnTimeout defaults to "block" for blocking events (fail-closed) and to
// "allow" for non-blocking ones.
func TestValidate_AppliesOnTimeoutDefault(t *testing.T) {
	h := baseValidCommandHook()
	h.OnTimeout = ""
	h.Event = hooks.EventPreToolUse // blocking
	if err := h.Validate(edition.Lite); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if h.OnTimeout != hooks.DecisionBlock {
		t.Errorf("OnTimeout default for blocking event = %q, want %q", h.OnTimeout, hooks.DecisionBlock)
	}

	h2 := baseValidCommandHook()
	h2.OnTimeout = ""
	h2.Event = hooks.EventPostToolUse // non-blocking
	if err := h2.Validate(edition.Lite); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if h2.OnTimeout != hooks.DecisionAllow {
		t.Errorf("OnTimeout default for non-blocking event = %q, want %q", h2.OnTimeout, hooks.DecisionAllow)
	}
}

// Version defaults to 1 when zero — initial Create path.
func TestValidate_AppliesVersionDefault(t *testing.T) {
	h := baseValidCommandHook()
	h.Version = 0
	if err := h.Validate(edition.Lite); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if h.Version != 1 {
		t.Errorf("Version default = %d, want 1", h.Version)
	}
}

// Source defaults to "ui" when empty — distinguishes UI writes from agent-seeded
// hooks (M1 isolation in plan).
func TestValidate_AppliesSourceDefault(t *testing.T) {
	h := baseValidCommandHook()
	h.Source = ""
	if err := h.Validate(edition.Lite); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if h.Source != "ui" {
		t.Errorf("Source default = %q, want %q", h.Source, "ui")
	}
}

func TestValidate_RejectsUnknownEvent(t *testing.T) {
	h := baseValidCommandHook()
	h.Event = hooks.HookEvent("webhook_magic")
	err := h.Validate(edition.Lite)
	if err == nil {
		t.Fatal("expected error for unknown event")
	}
}

func TestValidate_RejectsGlobalScopeWithNonSentinelTenant(t *testing.T) {
	h := baseValidCommandHook()
	h.Scope = hooks.ScopeGlobal
	h.TenantID = uuid.New() // must be sentinel for global
	err := h.Validate(edition.Lite)
	if err == nil {
		t.Fatal("expected error for global scope with non-sentinel tenant")
	}
}

func TestValidate_RejectsAgentScopeWithoutAgentID(t *testing.T) {
	h := baseValidCommandHook()
	h.Scope = hooks.ScopeAgent
	h.TenantID = uuid.New()
	h.AgentID = nil
	err := h.Validate(edition.Lite)
	if err == nil {
		t.Fatal("expected error for agent scope without agent_id")
	}
}

func TestValidate_RejectsNegativeTimeout(t *testing.T) {
	h := baseValidCommandHook()
	h.TimeoutMS = -1
	err := h.Validate(edition.Lite)
	if err == nil {
		t.Fatal("expected error for negative timeout")
	}
}

func TestValidate_CapsTimeoutToMax(t *testing.T) {
	h := baseValidCommandHook()
	h.TimeoutMS = 60 * 1000 // 60s — exceeds chain wall-time budget
	err := h.Validate(edition.Lite)
	if err == nil {
		t.Fatal("expected error for timeout > MaxTimeoutMS")
	}
}

// ─── Script handler validation (Phase 03) ────────────────────────────────────

func baseValidScriptHook() hooks.HookConfig {
	h := baseValidCommandHook()
	h.HandlerType = hooks.HandlerScript
	h.Config = map[string]any{"source": `function handle(e){return {decision:"allow"}}`}
	return h
}

func TestValidate_AcceptsValidScriptHook(t *testing.T) {
	h := baseValidScriptHook()
	if err := h.Validate(edition.Standard); err != nil {
		t.Fatalf("expected nil error; got %v", err)
	}
}

func TestValidate_RejectsEmptyScriptSource(t *testing.T) {
	h := baseValidScriptHook()
	h.Config = map[string]any{"source": ""}
	err := h.Validate(edition.Standard)
	if err == nil || !strings.Contains(err.Error(), "non-empty") {
		t.Fatalf("expected non-empty source error; got %v", err)
	}
}

func TestValidate_RejectsOversizedScriptSource(t *testing.T) {
	h := baseValidScriptHook()
	big := strings.Repeat("// pad\n", 6000) // >32 KiB
	h.Config = map[string]any{"source": big}
	err := h.Validate(edition.Standard)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected exceeds-bytes error; got %v", err)
	}
}

func TestValidate_RejectsScriptCompileError(t *testing.T) {
	h := baseValidScriptHook()
	// Missing closing brace → parser error with line:col.
	h.Config = map[string]any{"source": `function handle(e) { return {`}
	err := h.Validate(edition.Standard)
	if err == nil || !strings.Contains(err.Error(), "compile error") {
		t.Fatalf("expected compile error; got %v", err)
	}
}

func TestValidate_RejectsOnTimeoutAsk(t *testing.T) {
	h := baseValidScriptHook()
	h.OnTimeout = hooks.DecisionAsk
	err := h.Validate(edition.Standard)
	if err == nil || !strings.Contains(err.Error(), "on_timeout") {
		t.Fatalf("expected on_timeout rejection; got %v", err)
	}
}

func TestValidate_RejectsOnTimeoutDefer(t *testing.T) {
	h := baseValidScriptHook()
	h.OnTimeout = hooks.DecisionDefer
	err := h.Validate(edition.Standard)
	if err == nil || !strings.Contains(err.Error(), "on_timeout") {
		t.Fatalf("expected on_timeout rejection; got %v", err)
	}
}

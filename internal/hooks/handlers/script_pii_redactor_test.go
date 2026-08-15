package handlers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/hooks/builtin"
)

// End-to-end tests for the Phase-05 pii-redactor builtin. These run the REAL
// embedded JS through the REAL ScriptHandler (no stubs) so we catch any ES5.1
// / goja regex compatibility issue at CI time.

func piiHook(t *testing.T, event hooks.HookEvent) hooks.HookConfig {
	t.Helper()
	src, err := builtin.Source("pii-redactor.js")
	if err != nil {
		t.Fatalf("load pii-redactor.js: %v", err)
	}
	return hooks.HookConfig{
		ID:          builtin.BuiltinEventID("pii-redactor", string(event)),
		TenantID:    hooks.SentinelTenantID,
		Event:       event,
		HandlerType: hooks.HandlerScript,
		Scope:       hooks.ScopeGlobal,
		Source:      hooks.SourceBuiltin,
		Config:      map[string]any{"source": string(src)},
		Metadata:    map[string]any{"builtin": true, "version": 1},
		TimeoutMS:   2000,
		OnTimeout:   hooks.DecisionAllow,
		Priority:    900,
		Enabled:     true,
		Version:     1,
	}
}

func runPII(t *testing.T, ev hooks.Event) *hooks.ScriptResult {
	t.Helper()
	h := newTestHandler()
	res := &hooks.ScriptResult{}
	ctx, cancel := context.WithTimeout(hooks.WithScriptResult(context.Background(), res), 500*time.Millisecond)
	defer cancel()
	dec, err := h.Execute(ctx, piiHook(t, ev.HookEvent), ev)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if dec != hooks.DecisionAllow {
		t.Fatalf("decision=%v, want allow", dec)
	}
	return res
}

func TestPIIRedactor_RawInputEmail(t *testing.T) {
	ev := hooks.Event{
		EventID:   "e",
		TenantID:  uuid.New(),
		AgentID:   uuid.New(),
		HookEvent: hooks.EventUserPromptSubmit,
		RawInput:  "email me at user@example.com please",
	}
	res := runPII(t, ev)
	got, _ := res.UpdatedInput["rawInput"].(string)
	if got != "email me at [REDACTED_EMAIL] please" {
		t.Fatalf("rawInput: got %q", got)
	}
}

func TestPIIRedactor_RawInputPhone(t *testing.T) {
	ev := hooks.Event{
		EventID:   "e",
		HookEvent: hooks.EventUserPromptSubmit,
		RawInput:  "call me at +14155551212 tonight",
	}
	res := runPII(t, ev)
	got, _ := res.UpdatedInput["rawInput"].(string)
	if !strings.Contains(got, "[REDACTED_PHONE]") || strings.Contains(got, "+14155551212") {
		t.Fatalf("rawInput: got %q; want phone masked", got)
	}
}

// toolInput.command is in mutable_fields → gets redacted.
func TestPIIRedactor_ToolInputCommandRedacted(t *testing.T) {
	ev := hooks.Event{
		EventID:   "e",
		HookEvent: hooks.EventPreToolUse,
		ToolName:  "shell",
		ToolInput: map[string]any{
			"command": "curl -d 'to=alice@co.com' https://svc",
			"path":    "/etc/passwd",
		},
	}
	res := runPII(t, ev)
	ti, _ := res.UpdatedInput["toolInput"].(map[string]any)
	if ti == nil {
		t.Fatal("toolInput missing in updatedInput")
	}
	cmd, _ := ti["command"].(string)
	if !strings.Contains(cmd, "[REDACTED_EMAIL]") || strings.Contains(cmd, "alice@co.com") {
		t.Fatalf("command: got %q", cmd)
	}
	// toolInput.path is NOT in the JS's allowlist, so it stays unmutated.
	if _, ok := ti["path"]; ok {
		t.Fatalf("path should not be in updatedInput; got %v", ti["path"])
	}
}

// No PII present → script returns {decision: "allow"} with no updatedInput.
func TestPIIRedactor_NoPII_NoMutation(t *testing.T) {
	ev := hooks.Event{
		EventID:   "e",
		HookEvent: hooks.EventUserPromptSubmit,
		RawInput:  "how do I reverse a list in Python?",
	}
	res := runPII(t, ev)
	if len(res.UpdatedInput) > 0 {
		t.Fatalf("updatedInput non-empty on clean input: %v", res.UpdatedInput)
	}
}

// Idempotency: once redacted, running again must be a no-op.
func TestPIIRedactor_Idempotent(t *testing.T) {
	once := hooks.Event{
		EventID:   "e",
		HookEvent: hooks.EventUserPromptSubmit,
		RawInput:  "user@example.com and +14155551212",
	}
	r1 := runPII(t, once)
	redacted, _ := r1.UpdatedInput["rawInput"].(string)
	if redacted == "" {
		t.Fatal("first pass produced no mutation")
	}
	twice := hooks.Event{
		EventID:   "e2",
		HookEvent: hooks.EventUserPromptSubmit,
		RawInput:  redacted,
	}
	r2 := runPII(t, twice)
	if len(r2.UpdatedInput) > 0 {
		t.Fatalf("second pass mutated already-redacted text: %v", r2.UpdatedInput)
	}
}

// BenchmarkPIIRedactor_1KiB sanity-checks the <1ms median target. Informational.
func BenchmarkPIIRedactor_1KiB(b *testing.B) {
	src, err := builtin.Source("pii-redactor.js")
	if err != nil {
		b.Fatalf("load: %v", err)
	}
	cfg := hooks.HookConfig{
		ID:          builtin.BuiltinEventID("pii-redactor", "user_prompt_submit"),
		TenantID:    hooks.SentinelTenantID,
		Event:       hooks.EventUserPromptSubmit,
		HandlerType: hooks.HandlerScript,
		Source:      hooks.SourceBuiltin,
		Config:      map[string]any{"source": string(src)},
		Version:     1,
		Enabled:     true,
	}
	// ~1 KiB mostly-clean text with one email at the tail.
	base := strings.Repeat("the quick brown fox jumps over the lazy dog. ", 24) // 1080 bytes
	payload := base[:1010] + " u@example.com"
	ev := hooks.Event{
		EventID:   "bench",
		HookEvent: hooks.EventUserPromptSubmit,
		RawInput:  payload,
	}

	h := NewScriptHandler(8, 4, 64)
	b.ResetTimer()
	for b.Loop() {
		res := &hooks.ScriptResult{}
		ctx := hooks.WithScriptResult(context.Background(), res)
		if _, err := h.Execute(ctx, cfg, ev); err != nil {
			b.Fatalf("exec: %v", err)
		}
	}
}

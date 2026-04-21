package handlers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

// newTestHandler constructs a handler with small caps tuned for unit tests.
func newTestHandler() *ScriptHandler {
	return NewScriptHandler(4, 2, 32)
}

// mkCfg builds a minimal HookConfig carrying only what the script handler reads.
func mkCfg(source string) hooks.HookConfig {
	return hooks.HookConfig{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		Event:       hooks.EventUserPromptSubmit,
		HandlerType: hooks.HandlerScript,
		Scope:       hooks.ScopeTenant,
		Config:      map[string]any{"source": source},
		Version:     1,
		Enabled:     true,
	}
}

// mkEvent builds a minimal Event with non-nil maps so the frozen binding has
// something to iterate.
func mkEvent() hooks.Event {
	return hooks.Event{
		EventID:   "evt-1",
		SessionID: "sess-1",
		TenantID:  uuid.New(),
		AgentID:   uuid.New(),
		ToolName:  "test_tool",
		ToolInput: map[string]any{"path": "/tmp/x", "n": 3},
		RawInput:  "hello",
		HookEvent: hooks.EventUserPromptSubmit,
	}
}

// runWithResult executes and returns the decision plus the ScriptResult the
// handler populated via the ctx-carried pointer.
func runWithResult(t *testing.T, h *ScriptHandler, cfg hooks.HookConfig, ev hooks.Event, timeout time.Duration) (hooks.Decision, error, *hooks.ScriptResult) {
	t.Helper()
	res := &hooks.ScriptResult{}
	ctx, cancel := context.WithTimeout(hooks.WithScriptResult(context.Background(), res), timeout)
	defer cancel()
	d, err := h.Execute(ctx, cfg, ev)
	return d, err, res
}

// TestHappyPathAllow verifies a script returning {decision:"allow",reason:"ok"}
// produces DecisionAllow and the reason flows to ScriptResult.
func TestHappyPathAllow(t *testing.T) {
	src := `function handle(event) { return {decision: "allow", reason: "ok"}; }`
	h := newTestHandler()
	dec, err, res := runWithResult(t, h, mkCfg(src), mkEvent(), 500*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if dec != hooks.DecisionAllow {
		t.Fatalf("decision: got %v, want allow", dec)
	}
	if res.Reason != "ok" {
		t.Fatalf("reason: got %q, want %q", res.Reason, "ok")
	}
}

// TestHappyPathBlock verifies block + reason round-trips.
func TestHappyPathBlock(t *testing.T) {
	src := `function handle(event) { return {decision: "block", reason: "nope"}; }`
	h := newTestHandler()
	dec, err, res := runWithResult(t, h, mkCfg(src), mkEvent(), 500*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if dec != hooks.DecisionBlock {
		t.Fatalf("decision: got %v, want block", dec)
	}
	if res.Reason != "nope" {
		t.Fatalf("reason: got %q", res.Reason)
	}
}

// TestMissingHandleFunction yields DecisionError when the script omits `handle`.
func TestMissingHandleFunction(t *testing.T) {
	src := `var x = 1;`
	h := newTestHandler()
	dec, err, _ := runWithResult(t, h, mkCfg(src), mkEvent(), 500*time.Millisecond)
	if dec != hooks.DecisionError {
		t.Fatalf("decision: got %v, want error", dec)
	}
	if err == nil || !strings.Contains(err.Error(), "handle") {
		t.Fatalf("err: %v (want mentions 'handle')", err)
	}
}

// TestNonObjectReturn yields DecisionError.
func TestNonObjectReturn(t *testing.T) {
	src := `function handle(event) { return 42; }`
	h := newTestHandler()
	dec, err, _ := runWithResult(t, h, mkCfg(src), mkEvent(), 500*time.Millisecond)
	if dec != hooks.DecisionError {
		t.Fatalf("decision: got %v, want error (err=%v)", dec, err)
	}
}

// TestInvalidDecisionString yields DecisionError with message containing the bad string.
func TestInvalidDecisionString(t *testing.T) {
	src := `function handle(event) { return {decision: "maybe"}; }`
	h := newTestHandler()
	dec, err, _ := runWithResult(t, h, mkCfg(src), mkEvent(), 500*time.Millisecond)
	if dec != hooks.DecisionError {
		t.Fatalf("decision: got %v", dec)
	}
	if err == nil || !strings.Contains(err.Error(), "maybe") {
		t.Fatalf("err should mention the invalid value, got %v", err)
	}
}

// TestTimeoutInfiniteLoop verifies ctx cancellation interrupts a runaway script.
func TestTimeoutInfiniteLoop(t *testing.T) {
	src := `function handle(event) { while(true){} }`
	h := newTestHandler()
	dec, err, _ := runWithResult(t, h, mkCfg(src), mkEvent(), 100*time.Millisecond)
	if dec != hooks.DecisionTimeout {
		t.Fatalf("decision: got %v, want timeout (err=%v)", dec, err)
	}
}

// TestPanicInsideHandle surfaces as DecisionError with sanitized message
// (no newline, no " at " frame).
func TestPanicInsideHandle(t *testing.T) {
	src := `function handle(event) { throw new Error("boom"); }`
	h := newTestHandler()
	dec, err, _ := runWithResult(t, h, mkCfg(src), mkEvent(), 500*time.Millisecond)
	if dec != hooks.DecisionError {
		t.Fatalf("decision: got %v", dec)
	}
	if err == nil {
		t.Fatal("err: nil, want non-nil")
	}
	if strings.Contains(err.Error(), "\n") {
		t.Fatalf("err leaks multi-line stack trace: %q", err.Error())
	}
	if strings.Contains(err.Error(), " at ") {
		t.Fatalf("err leaks stack frame text: %q", err.Error())
	}
}

// TestAskDecisionBlocksInWave1 verifies ask/defer map to block.
func TestAskDecisionBlocksInWave1(t *testing.T) {
	src := `function handle(event) { return {decision: "ask", reason: "pls"}; }`
	h := newTestHandler()
	dec, err, res := runWithResult(t, h, mkCfg(src), mkEvent(), 500*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if dec != hooks.DecisionBlock {
		t.Fatalf("decision: got %v, want block (ask→block in Wave 1)", dec)
	}
	if res.Reason != "pls" {
		t.Fatalf("reason lost: %q", res.Reason)
	}
}

// TestDeferDecisionBlocksInWave1 mirror of the ask case.
func TestDeferDecisionBlocksInWave1(t *testing.T) {
	src := `function handle(event) { return {decision: "defer"}; }`
	h := newTestHandler()
	dec, _, _ := runWithResult(t, h, mkCfg(src), mkEvent(), 500*time.Millisecond)
	if dec != hooks.DecisionBlock {
		t.Fatalf("decision: got %v, want block (defer→block in Wave 1)", dec)
	}
}

// TestSourceOverSizeCap rejects scripts larger than MaxScriptSourceBytes.
func TestSourceOverSizeCap(t *testing.T) {
	big := strings.Repeat("// padding ", MaxScriptSourceBytes/10+10) // >32KiB
	src := `function handle(event){return {decision:"allow"}}` + "\n" + big
	h := newTestHandler()
	dec, err, _ := runWithResult(t, h, mkCfg(src), mkEvent(), 500*time.Millisecond)
	if dec != hooks.DecisionError {
		t.Fatalf("decision: got %v, want error", dec)
	}
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err: %v", err)
	}
}

// TestStdoutCapTruncates verifies the 4KiB ceiling cuts off runaway logging
// with a single truncation marker.
func TestStdoutCapTruncates(t *testing.T) {
	src := `function handle(event) {
	  for (var i = 0; i < 5000; i++) { console.log("xxxxxxxxxx"); }
	  return {decision: "allow"};
	}`
	h := newTestHandler()
	dec, err, res := runWithResult(t, h, mkCfg(src), mkEvent(), 2*time.Second)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if dec != hooks.DecisionAllow {
		t.Fatalf("decision: got %v", dec)
	}
	if len(res.Stdout) > MaxStdoutBytes+64 { // +64 slack for marker tail
		t.Fatalf("stdout exceeded cap: %d bytes", len(res.Stdout))
	}
	if !strings.Contains(res.Stdout, "truncated") {
		end := min(200, len(res.Stdout))
		t.Fatalf("truncation marker missing: %q", res.Stdout[:end])
	}
}

// TestUpdatedInputCaptured verifies updatedInput flows back via ScriptResult.
func TestUpdatedInputCaptured(t *testing.T) {
	src := `function handle(event) {
	  return {decision: "allow", updatedInput: {path: "/redacted"}};
	}`
	h := newTestHandler()
	dec, err, res := runWithResult(t, h, mkCfg(src), mkEvent(), 500*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if dec != hooks.DecisionAllow {
		t.Fatalf("decision: got %v", dec)
	}
	got, _ := res.UpdatedInput["path"].(string)
	if got != "/redacted" {
		t.Fatalf("updatedInput.path: got %q, want /redacted", got)
	}
}

// TestInvalidateHookDoesNotPanic is a smoke test for the Phase-03 wire-up point.
func TestInvalidateHookDoesNotPanic(t *testing.T) {
	h := newTestHandler()
	cfg := mkCfg(`function handle(event){return {decision:"allow"}}`)
	if _, err := h.Execute(context.Background(), cfg, mkEvent()); err != nil {
		t.Fatalf("warmup err: %v", err)
	}
	h.InvalidateHook(cfg.ID)
	// Re-execute to ensure cache repopulates cleanly.
	if _, err := h.Execute(context.Background(), cfg, mkEvent()); err != nil {
		t.Fatalf("post-invalidate err: %v", err)
	}
}

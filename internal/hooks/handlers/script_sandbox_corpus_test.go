package handlers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
)

// The corpus asserts the sandbox either:
//   (a) denies an escape primitive — the script compiles but fails at runtime
//       (DecisionError) with NO leaked sensitive global reference, OR
//   (b) bounds a resource bomb within ctx timeout (DecisionTimeout) WITHOUT
//       crashing the Go test process, OR
//   (c) for typeof-sanity cases, the script completes with decision:"allow"
//       proving the referenced global is `undefined` post-hardening.
//
// Each case wraps the attacker payload in `handle(event)` and runs under a
// short ctx deadline. The handler is shared per-test to validate the
// sandbox resets per execution.

// runCorpus executes src under a fresh handler instance and returns decision+err.
// perCaseTimeout bounds resource-bomb cases; happy cases finish well under it.
func runCorpus(t *testing.T, h *ScriptHandler, src string, perCaseTimeout time.Duration) (hooks.Decision, error) {
	t.Helper()
	cfg := hooks.HookConfig{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		Event:       hooks.EventUserPromptSubmit,
		HandlerType: hooks.HandlerScript,
		Scope:       hooks.ScopeTenant,
		Config:      map[string]any{"source": src},
		Version:     1,
		Enabled:     true,
	}
	ev := hooks.Event{
		EventID:   "corpus",
		SessionID: "s",
		TenantID:  uuid.New(),
		AgentID:   uuid.New(),
		ToolName:  "t",
		ToolInput: map[string]any{"secret": "shh"},
		HookEvent: hooks.EventUserPromptSubmit,
	}
	ctx, cancel := context.WithTimeout(context.Background(), perCaseTimeout)
	defer cancel()
	return h.Execute(ctx, cfg, ev)
}

// wrapAllowIf returns a `handle` that returns allow only when cond is truthy,
// block otherwise — used for typeof-sanity cases so a false proves the global
// wasn't actually undefined.
func wrapAllowIf(cond string) string {
	return `function handle(e) { return {decision: (` + cond + `) ? "allow" : "block", reason: "probe"}; }`
}

// ─── Escape-primitive bypasses (must fail or return block/error) ────────────

func TestCorpus_ConstructorChain(t *testing.T) {
	src := `function handle(e) { var r = [].constructor.constructor("return 1")(); return {decision: "allow", reason: String(r)}; }`
	dec, err := runCorpus(t, newTestHandler(), src, 500*time.Millisecond)
	if dec == hooks.DecisionAllow {
		t.Fatalf("escape via [].constructor.constructor succeeded (err=%v)", err)
	}
}

func TestCorpus_ReflectConstruct(t *testing.T) {
	src := `function handle(e) { return {decision: "allow", reason: String(Reflect.construct(Function, ["return 1"])())}; }`
	dec, _ := runCorpus(t, newTestHandler(), src, 500*time.Millisecond)
	if dec == hooks.DecisionAllow {
		t.Fatal("Reflect.construct bypass succeeded")
	}
}

func TestCorpus_ReflectApply(t *testing.T) {
	src := `function handle(e) { return {decision: "allow", reason: String(Reflect.apply([].constructor.constructor, null, ["return 1"])())}; }`
	dec, _ := runCorpus(t, newTestHandler(), src, 500*time.Millisecond)
	if dec == hooks.DecisionAllow {
		t.Fatal("Reflect.apply bypass succeeded")
	}
}

func TestCorpus_ProxyTrap(t *testing.T) {
	src := `function handle(e) { return {decision: "allow", reason: String(new Proxy({}, {get: function(){return "x"}}).anything)}; }`
	dec, _ := runCorpus(t, newTestHandler(), src, 500*time.Millisecond)
	if dec == hooks.DecisionAllow {
		t.Fatal("Proxy trap bypass succeeded")
	}
}

func TestCorpus_SymbolHijack(t *testing.T) {
	src := `function handle(e) { var s = Symbol("x"); return {decision: "allow", reason: String(s)}; }`
	dec, _ := runCorpus(t, newTestHandler(), src, 500*time.Millisecond)
	if dec == hooks.DecisionAllow {
		t.Fatal("Symbol hijack succeeded")
	}
}

func TestCorpus_PromiseQueue(t *testing.T) {
	src := `function handle(e) { Promise.resolve(); return {decision: "allow"}; }`
	dec, _ := runCorpus(t, newTestHandler(), src, 500*time.Millisecond)
	if dec == hooks.DecisionAllow {
		t.Fatal("Promise visible post-deny")
	}
}

func TestCorpus_ProtoWalk(t *testing.T) {
	// After prototype nullify, ({}).__proto__.constructor should be undefined.
	src := `function handle(e) {
	  var c = ({}).__proto__.constructor;
	  if (typeof c === "function") {
	    var f = c("return 1")(); // escape
	    return {decision: "allow", reason: String(f)};
	  }
	  return {decision: "block", reason: "proto walk blocked"};
	}`
	dec, _ := runCorpus(t, newTestHandler(), src, 500*time.Millisecond)
	if dec == hooks.DecisionAllow {
		t.Fatal("__proto__ walk yielded allow (escape succeeded)")
	}
}

func TestCorpus_DirectEval(t *testing.T) {
	src := `function handle(e) { return {decision: "allow", reason: String(eval("1+1"))}; }`
	dec, _ := runCorpus(t, newTestHandler(), src, 500*time.Millisecond)
	if dec == hooks.DecisionAllow {
		t.Fatal("direct eval succeeded")
	}
}

func TestCorpus_FunctionConstructor(t *testing.T) {
	src := `function handle(e) { return {decision: "allow", reason: String(new Function("return 1")())}; }`
	dec, _ := runCorpus(t, newTestHandler(), src, 500*time.Millisecond)
	if dec == hooks.DecisionAllow {
		t.Fatal("Function constructor visible")
	}
}

func TestCorpus_DateConstructorEscape(t *testing.T) {
	src := `function handle(e) { return {decision: "allow", reason: String((new Date()).constructor.constructor("return 1")())}; }`
	dec, _ := runCorpus(t, newTestHandler(), src, 500*time.Millisecond)
	if dec == hooks.DecisionAllow {
		t.Fatal("Date constructor escape succeeded")
	}
}

func TestCorpus_JSONReplacerInfinity(t *testing.T) {
	// Replacer returning 1/0 (Infinity) forces JSON to emit null. Not an escape
	// — just verifies exec completes without crash.
	src := `function handle(e) {
	  var s = JSON.stringify({x: 1}, function(k,v){ return v === this ? 1/0 : v; });
	  return {decision: "allow", reason: s};
	}`
	dec, err := runCorpus(t, newTestHandler(), src, 500*time.Millisecond)
	if dec != hooks.DecisionAllow && dec != hooks.DecisionError {
		t.Fatalf("unexpected decision %v err=%v", dec, err)
	}
}

func TestCorpus_ToJSONDoS(t *testing.T) {
	src := `function handle(e) {
	  var o = {};
	  o.toJSON = function(){ while(true){} };
	  return {decision: "allow", reason: JSON.stringify(o)};
	}`
	dec, _ := runCorpus(t, newTestHandler(), src, 100*time.Millisecond)
	if dec != hooks.DecisionTimeout {
		t.Fatalf("toJSON DoS: got %v, want timeout", dec)
	}
}

// ─── Resource bombs (must timeout / bounded error, no host crash) ────────────

func TestCorpus_RecursionBomb(t *testing.T) {
	src := `function handle(e) { function f(){ f(); } f(); }`
	dec, err := runCorpus(t, newTestHandler(), src, 500*time.Millisecond)
	if dec != hooks.DecisionError {
		t.Fatalf("recursion bomb: got %v (err=%v), want error", dec, err)
	}
}

func TestCorpus_InfiniteLoop(t *testing.T) {
	src := `function handle(e) { while(true){} }`
	dec, _ := runCorpus(t, newTestHandler(), src, 100*time.Millisecond)
	if dec != hooks.DecisionTimeout {
		t.Fatalf("infinite loop: got %v, want timeout", dec)
	}
}

func TestCorpus_MemoryBombArray(t *testing.T) {
	src := `function handle(e) {
	  try { var a = new Array(1e9); return {decision:"allow", reason: String(a.length)}; }
	  catch (err) { return {decision:"error"}; }
	}`
	dec, _ := runCorpus(t, newTestHandler(), src, 500*time.Millisecond)
	_ = dec // absence-of-crash is the real assertion
}

func TestCorpus_MemoryBombString(t *testing.T) {
	src := `function handle(e) {
	  try { var s = "x".repeat(1e9); return {decision:"allow", reason: String(s.length)}; }
	  catch (err) { return {decision:"error"}; }
	}`
	dec, _ := runCorpus(t, newTestHandler(), src, 500*time.Millisecond)
	if dec == hooks.DecisionAllow {
		t.Fatal("1GiB repeat succeeded — sandbox did not bound string allocation")
	}
}

func TestCorpus_ReDoS(t *testing.T) {
	// Catastrophic backtracking pattern. Goja uses dlclark/regexp2 which can
	// be backtracking-heavy on some ES-specific features, but for simple
	// cases it may return linearly. Any terminal decision within ctx deadline
	// proves the sandbox bounded the regex — the real failure mode would be
	// a hung goroutine.
	src := `function handle(e) {
	  var r = new RegExp("(a+)+$");
	  r.test("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab");
	  return {decision: "allow"};
	}`
	dec, _ := runCorpus(t, newTestHandler(), src, 500*time.Millisecond)
	switch dec {
	case hooks.DecisionAllow, hooks.DecisionTimeout, hooks.DecisionError:
		// ok — bounded either by linear engine or by timeout
	default:
		t.Fatalf("ReDoS: got %v, want allow/timeout/error", dec)
	}
}

// ─── Typeof sanity (post-hardening each global is `undefined`) ───────────────

func TestCorpus_ReflectUndefined(t *testing.T) {
	dec, err := runCorpus(t, newTestHandler(), wrapAllowIf(`typeof Reflect === "undefined"`), 200*time.Millisecond)
	if dec != hooks.DecisionAllow {
		t.Fatalf("Reflect should be undefined (dec=%v err=%v)", dec, err)
	}
}

func TestCorpus_ProxyUndefined(t *testing.T) {
	dec, err := runCorpus(t, newTestHandler(), wrapAllowIf(`typeof Proxy === "undefined"`), 200*time.Millisecond)
	if dec != hooks.DecisionAllow {
		t.Fatalf("Proxy should be undefined (dec=%v err=%v)", dec, err)
	}
}

func TestCorpus_SymbolUndefined(t *testing.T) {
	dec, err := runCorpus(t, newTestHandler(), wrapAllowIf(`typeof Symbol === "undefined"`), 200*time.Millisecond)
	if dec != hooks.DecisionAllow {
		t.Fatalf("Symbol should be undefined (dec=%v err=%v)", dec, err)
	}
}

func TestCorpus_PromiseUndefined(t *testing.T) {
	dec, err := runCorpus(t, newTestHandler(), wrapAllowIf(`typeof Promise === "undefined"`), 200*time.Millisecond)
	if dec != hooks.DecisionAllow {
		t.Fatalf("Promise should be undefined (dec=%v err=%v)", dec, err)
	}
}

func TestCorpus_GoErrorUndefined(t *testing.T) {
	dec, err := runCorpus(t, newTestHandler(), wrapAllowIf(`typeof GoError === "undefined"`), 200*time.Millisecond)
	if dec != hooks.DecisionAllow {
		t.Fatalf("GoError should be undefined (dec=%v err=%v)", dec, err)
	}
}

func TestCorpus_GlobalThisUndefined(t *testing.T) {
	dec, err := runCorpus(t, newTestHandler(), wrapAllowIf(`typeof globalThis === "undefined"`), 200*time.Millisecond)
	if dec != hooks.DecisionAllow {
		t.Fatalf("globalThis should be undefined (dec=%v err=%v)", dec, err)
	}
}

func TestCorpus_EvalUndefined(t *testing.T) {
	dec, err := runCorpus(t, newTestHandler(), wrapAllowIf(`typeof eval === "undefined"`), 200*time.Millisecond)
	if dec != hooks.DecisionAllow {
		t.Fatalf("eval should be undefined (dec=%v err=%v)", dec, err)
	}
}

func TestCorpus_FunctionUndefined(t *testing.T) {
	dec, err := runCorpus(t, newTestHandler(), wrapAllowIf(`typeof Function === "undefined"`), 200*time.Millisecond)
	if dec != hooks.DecisionAllow {
		t.Fatalf("Function should be undefined (dec=%v err=%v)", dec, err)
	}
}

// ─── Mutation defense (H1: deep-freeze of the event object) ──────────────────

func TestCorpus_MutationOnToolInputFrozen(t *testing.T) {
	src := `function handle(e) {
	  try { e.toolInput.secret = "pwn"; } catch(_) {}
	  return {decision: "allow", reason: String(e.toolInput.secret)};
	}`
	h := newTestHandler()
	cfg := hooks.HookConfig{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		Event:       hooks.EventUserPromptSubmit,
		HandlerType: hooks.HandlerScript,
		Scope:       hooks.ScopeTenant,
		Config:      map[string]any{"source": src},
		Version:     1,
	}
	ev := hooks.Event{
		EventID:   "mut",
		SessionID: "s",
		TenantID:  uuid.New(),
		AgentID:   uuid.New(),
		ToolName:  "t",
		ToolInput: map[string]any{"secret": "original"},
		HookEvent: hooks.EventUserPromptSubmit,
	}
	res := &hooks.ScriptResult{}
	ctx, cancel := context.WithTimeout(hooks.WithScriptResult(context.Background(), res), 500*time.Millisecond)
	defer cancel()
	dec, err := h.Execute(ctx, cfg, ev)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if dec != hooks.DecisionAllow {
		t.Fatalf("decision: got %v", dec)
	}
	if ev.ToolInput["secret"] != "original" {
		t.Fatalf("Go-side toolInput mutated: %v", ev.ToolInput["secret"])
	}
	if !strings.Contains(res.Reason, "original") {
		t.Fatalf("JS observed mutated value: reason=%q", res.Reason)
	}
}

func TestCorpus_MutationOnRawInputFrozen(t *testing.T) {
	src := `function handle(e) {
	  try { e.rawInput = "pwn"; } catch(_) {}
	  return {decision: "allow", reason: e.rawInput};
	}`
	h := newTestHandler()
	cfg := hooks.HookConfig{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		Event:       hooks.EventUserPromptSubmit,
		HandlerType: hooks.HandlerScript,
		Scope:       hooks.ScopeTenant,
		Config:      map[string]any{"source": src},
		Version:     1,
	}
	ev := hooks.Event{
		EventID:   "mut2",
		SessionID: "s",
		TenantID:  uuid.New(),
		AgentID:   uuid.New(),
		ToolName:  "t",
		RawInput:  "untouched",
		HookEvent: hooks.EventUserPromptSubmit,
	}
	res := &hooks.ScriptResult{}
	ctx, cancel := context.WithTimeout(hooks.WithScriptResult(context.Background(), res), 500*time.Millisecond)
	defer cancel()
	_, err := h.Execute(ctx, cfg, ev)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if ev.RawInput != "untouched" {
		t.Fatalf("Go-side rawInput mutated: %q", ev.RawInput)
	}
	if !strings.Contains(res.Reason, "untouched") {
		t.Fatalf("JS observed mutated value: reason=%q", res.Reason)
	}
}

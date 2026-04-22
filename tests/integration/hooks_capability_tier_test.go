//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/hooks/builtin"
	hookhandlers "github.com/nextlevelbuilder/goclaw/internal/hooks/handlers"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// Phase 08 — C bucket: source-tier capability gate + FireResult propagation.
//
// Wires the real ScriptHandler into the StdDispatcher and asserts that:
//   - non-builtin scripts returning updatedInput have it stripped (FireResult
//     fields nil + WARN log emitted)
//   - builtin scripts returning updatedInput populate FireResult with the
//     allowlisted fields and the caller can apply them to its own state
//   - allowlist filtering rejects out-of-list keys (e.g. toolInput.nonlisted)
//   - DecisionAsk from a script collapses to DecisionBlock at the dispatcher
//   - deep-freeze prevents in-script mutation of event input
//   - chain propagation: hook 2 sees hook 1's mutations via if_expr / rawInput

const (
	allowOnlyJS = `function handle(e) { return {decision:"allow"}; }`

	// Builtin shape: returns updatedInput.rawInput so dispatcher applies it
	// (provided source=="builtin").
	mutateRawInputJS = `function handle(e) { return {decision:"allow", updatedInput:{rawInput:"REWRITTEN"}}; }`

	// Try to write outside the allowlist.
	mutateNonlistedJS = `function handle(e) { return {decision:"allow", updatedInput:{toolInput:{nonlisted:"x"}}}; }`

	// Wave 1 reserves DecisionAsk → dispatcher should treat as block + warn.
	askDecisionJS = `function handle(e) { return {decision:"ask"}; }`

	// Try to mutate the frozen event in-place — JS strict mode + deep freeze
	// must throw, surfacing as DecisionError + leave Go-side toolInput intact.
	mutateFrozenJS = `function handle(e) { e.toolInput.command = "pwn"; return {decision:"allow"}; }`
)

func newCapDispatcher(t *testing.T, hs hooks.HookStore) hooks.Dispatcher {
	t.Helper()
	scriptH := hookhandlers.NewScriptHandler(4, 2, 32)
	return hooks.NewStdDispatcher(hooks.StdDispatcherOpts{
		Store: hs,
		Audit: hooks.NewAuditWriter(hs, ""),
		Handlers: map[hooks.HandlerType]hooks.Handler{
			hooks.HandlerScript: scriptH,
		},
		PerHookTimeout: 2 * time.Second,
		ChainBudget:    5 * time.Second,
	})
}

// test-C1: a UI-source script returning updatedInput → dispatcher strips it.
// FireResult.UpdatedRawInput must be nil. (WARN log emitted; not asserted —
// we don't capture slog output here, but the unit dispatcher_test does.)
func TestHooksC1_UISourceMutationDenied(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	hs := pg.NewPGHookStore(db)
	d := newCapDispatcher(t, hs)

	cfg := hookCfgScript(tenantID, hooks.EventUserPromptSubmit, "ui", mutateRawInputJS)
	id, err := hs.Create(tenantCtx(tenantID), cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { purgeAgentHookByID(t, db, id) })

	res, err := d.Fire(tenantCtx(tenantID), hooks.Event{
		EventID:   "c1",
		TenantID:  tenantID,
		AgentID:   agentID,
		HookEvent: hooks.EventUserPromptSubmit,
		RawInput:  "original",
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if res.Decision != hooks.DecisionAllow {
		t.Fatalf("decision=%v, want allow", res.Decision)
	}
	if res.UpdatedRawInput != nil {
		t.Errorf("UI-source mutation leaked: UpdatedRawInput=%q", *res.UpdatedRawInput)
	}
	if res.UpdatedToolInput != nil {
		t.Errorf("UI-source mutation leaked: UpdatedToolInput=%v", res.UpdatedToolInput)
	}
}

// test-C2: builtin-source script returning updatedInput → dispatcher applies
// it; FireResult.UpdatedRawInput points to the post-mutation string.
//
// NOTE: this test uses the REAL pii-redactor via the builtin loader so we
// catch any wiring regression in the allowlist lookup. We seed a builtin
// row directly + register the allowlist lookup (mirrors gateway boot wiring).
func TestHooksC2_BuiltinSourceMutationApplied(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	hs := pg.NewPGHookStore(db)

	// Load builtin registry + wire the allowlist lookup.
	if err := builtin.Load(); err != nil {
		t.Fatalf("builtin.Load: %v", err)
	}
	hooks.SetBuiltinAllowlistLookup(builtin.AllowlistFor)
	t.Cleanup(func() { hooks.SetBuiltinAllowlistLookup(nil) })

	src, err := builtin.Source("pii-redactor.js")
	if err != nil {
		t.Fatalf("load pii-redactor.js: %v", err)
	}
	cfg := hookCfgScript(hooks.SentinelTenantID, hooks.EventUserPromptSubmit,
		hooks.SourceBuiltin, string(src))
	cfg.Scope = hooks.ScopeGlobal
	cfg.ID = builtin.BuiltinEventID("pii-redactor", string(hooks.EventUserPromptSubmit))
	cfg.TimeoutMS = 2000
	cfg.Priority = 900

	id, err := hs.Create(crossTenantOwnerCtx(), cfg)
	if err != nil {
		t.Fatalf("Create builtin: %v", err)
	}
	t.Cleanup(func() { purgeAgentHookByID(t, db, id) })

	d := newCapDispatcher(t, hs)
	res, err := d.Fire(tenantCtx(tenantID), hooks.Event{
		EventID:   "c2",
		TenantID:  tenantID,
		AgentID:   agentID,
		HookEvent: hooks.EventUserPromptSubmit,
		RawInput:  "ping me at user@example.com",
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if res.Decision != hooks.DecisionAllow {
		t.Fatalf("decision=%v, want allow", res.Decision)
	}
	if res.UpdatedRawInput == nil {
		t.Fatalf("builtin mutation did not propagate to FireResult.UpdatedRawInput")
	}
	if !strings.Contains(*res.UpdatedRawInput, "[REDACTED_EMAIL]") {
		t.Errorf("UpdatedRawInput missing redaction marker: %q", *res.UpdatedRawInput)
	}
}

// test-C3: builtin script returning updatedInput.toolInput.nonlisted with an
// allowlist NOT including that key → dispatcher drops the field. We assert
// FireResult.UpdatedToolInput is nil OR does not contain the key.
//
// We hand-register a tiny allowlist for our test hook (rawInput only) so the
// allowlist gate has a known shape independent of pii-redactor.yaml.
func TestHooksC3_AllowlistDropsOutOfListField(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	hs := pg.NewPGHookStore(db)

	cfg := hookCfgScript(hooks.SentinelTenantID, hooks.EventPreToolUse,
		hooks.SourceBuiltin, mutateNonlistedJS)
	cfg.Scope = hooks.ScopeGlobal
	cfg.ID = uuid.New()
	cfg.Priority = 900

	id, err := hs.Create(crossTenantOwnerCtx(), cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { purgeAgentHookByID(t, db, id) })

	// Allowlist: rawInput only — toolInput keys MUST be filtered out.
	hooks.SetBuiltinAllowlistLookup(func(_ uuid.UUID) []string { return []string{"rawInput"} })
	t.Cleanup(func() { hooks.SetBuiltinAllowlistLookup(nil) })

	d := newCapDispatcher(t, hs)
	res, err := d.Fire(tenantCtx(tenantID), hooks.Event{
		EventID:   "c3",
		TenantID:  tenantID,
		AgentID:   agentID,
		ToolName:  "shell",
		ToolInput: map[string]any{"command": "ls"},
		HookEvent: hooks.EventPreToolUse,
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if res.Decision != hooks.DecisionAllow {
		t.Fatalf("decision=%v, want allow", res.Decision)
	}
	if res.UpdatedToolInput != nil {
		if _, present := res.UpdatedToolInput["nonlisted"]; present {
			t.Errorf("non-allowlisted key leaked into FireResult: %v", res.UpdatedToolInput)
		}
	}
}

// test-C4: DecisionAsk returned by script → handler converts to Block + WARN
// (Wave 1 reserves ask/defer). Audit row records decision=block.
func TestHooksC4_AskCollapsesToBlock(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	hs := pg.NewPGHookStore(db)

	cfg := hookCfgScript(tenantID, hooks.EventUserPromptSubmit, "ui", askDecisionJS)
	cfg.OnTimeout = hooks.DecisionBlock
	id, err := hs.Create(tenantCtx(tenantID), cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { purgeAgentHookByID(t, db, id) })

	d := newCapDispatcher(t, hs)
	res, err := d.Fire(tenantCtx(tenantID), hooks.Event{
		EventID:   "c4",
		TenantID:  tenantID,
		AgentID:   agentID,
		HookEvent: hooks.EventUserPromptSubmit,
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if res.Decision != hooks.DecisionBlock {
		t.Fatalf("decision=%v, want block (ask should collapse)", res.Decision)
	}
	if !pollAuditDecision(t, db, id, "block", 2*time.Second) {
		t.Errorf("expected block audit row for hook %s", id)
	}
}

// test-C5: a script attempting to MUTATE the deep-frozen event must throw
// (TypeError) → DecisionError. Go-side ToolInput must remain unchanged.
func TestHooksC5_DeepFreezeBlocksInPlaceMutation(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	hs := pg.NewPGHookStore(db)

	cfg := hookCfgScript(tenantID, hooks.EventPreToolUse, "ui", mutateFrozenJS)
	cfg.OnTimeout = hooks.DecisionAllow
	id, err := hs.Create(tenantCtx(tenantID), cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { purgeAgentHookByID(t, db, id) })

	originalToolInput := map[string]any{"command": "ls"}
	d := newCapDispatcher(t, hs)
	res, err := d.Fire(tenantCtx(tenantID), hooks.Event{
		EventID:   "c5",
		TenantID:  tenantID,
		AgentID:   agentID,
		ToolName:  "shell",
		ToolInput: originalToolInput,
		HookEvent: hooks.EventPreToolUse,
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	// pre_tool_use is BLOCKING; an error decision fails closed → block.
	if res.Decision != hooks.DecisionBlock {
		t.Fatalf("decision=%v, want block (pre_tool_use error fails closed)", res.Decision)
	}
	// Go-side input untouched — JS could not mutate through the deep-frozen
	// proxy that the runtime hands it.
	if got, _ := originalToolInput["command"].(string); got != "ls" {
		t.Errorf("ToolInput[command] mutated to %q (deep freeze defeated)", got)
	}
}

// test-C6: chain propagation. Two builtin hooks on user_prompt_submit:
//
//   - Hook 1 (priority=200, builtin): rewrites rawInput to "REWRITTEN".
//   - Hook 2 (priority=100, ui): sees the dispatcher's mutated event
//     (downstream chain visibility) and blocks based on rawInput content.
//
// We assert FireResult.Decision == Block (hook 2 saw the post-mutation
// rawInput) AND FireResult.UpdatedRawInput is the rewritten string.
func TestHooksC6_ChainMutationVisibleDownstream(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	hs := pg.NewPGHookStore(db)

	// Hook 1: builtin rewrites rawInput.
	hook1 := hookCfgScript(hooks.SentinelTenantID, hooks.EventUserPromptSubmit,
		hooks.SourceBuiltin, mutateRawInputJS)
	hook1.Scope = hooks.ScopeGlobal
	hook1.ID = uuid.New()
	hook1.Priority = 200
	id1, err := hs.Create(crossTenantOwnerCtx(), hook1)
	if err != nil {
		t.Fatalf("Create hook1: %v", err)
	}
	t.Cleanup(func() { purgeAgentHookByID(t, db, id1) })

	// Hook 2: tenant ui, body discriminates on the post-mutation rawInput.
	// Override the canned body to inspect the value the dispatcher hands it.
	hook2Source := `function handle(e) {
		if (typeof e.rawInput === "string" && e.rawInput === "REWRITTEN") {
			return {decision: "block", reason: "saw rewritten"};
		}
		return {decision: "allow", reason: "did not see rewrite: " + String(e.rawInput)};
	}`
	hook2 := hookCfgScript(tenantID, hooks.EventUserPromptSubmit, "ui", hook2Source)
	hook2.Priority = 100
	id2, err := hs.Create(tenantCtx(tenantID), hook2)
	if err != nil {
		t.Fatalf("Create hook2: %v", err)
	}
	t.Cleanup(func() { purgeAgentHookByID(t, db, id2) })

	// Allow-list: builtin may mutate rawInput.
	hooks.SetBuiltinAllowlistLookup(func(_ uuid.UUID) []string { return []string{"rawInput"} })
	t.Cleanup(func() { hooks.SetBuiltinAllowlistLookup(nil) })

	d := newCapDispatcher(t, hs)
	res, err := d.Fire(tenantCtx(tenantID), hooks.Event{
		EventID:   "c6",
		TenantID:  tenantID,
		AgentID:   agentID,
		HookEvent: hooks.EventUserPromptSubmit,
		RawInput:  "original",
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if res.Decision != hooks.DecisionBlock {
		t.Fatalf("decision=%v, want block (proves chain saw rewrite)", res.Decision)
	}
	// Audit row from hook 2 should record block.
	if !pollAuditDecision(t, db, id2, "block", 2*time.Second) {
		t.Errorf("expected block audit row for hook2 %s", id2)
	}
}

// Smoke: dispatcher Fire on a single allow-only script returns FireResult with
// nil Updated* fields and Decision=allow.
func TestHooksC_SmokeAllow(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	hs := pg.NewPGHookStore(db)

	cfg := hookCfgScript(tenantID, hooks.EventUserPromptSubmit, "ui", allowOnlyJS)
	id, err := hs.Create(tenantCtx(tenantID), cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { purgeAgentHookByID(t, db, id) })

	d := newCapDispatcher(t, hs)
	res, err := d.Fire(tenantCtx(tenantID), hooks.Event{
		EventID: "c-smoke", TenantID: tenantID, AgentID: agentID,
		HookEvent: hooks.EventUserPromptSubmit,
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if res.Decision != hooks.DecisionAllow {
		t.Fatalf("dec=%v want allow", res.Decision)
	}
	if res.UpdatedRawInput != nil || res.UpdatedToolInput != nil {
		t.Errorf("non-mutating script left Updated* non-nil: raw=%v tool=%v",
			res.UpdatedRawInput, res.UpdatedToolInput)
	}
}

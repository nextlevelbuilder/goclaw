//go:build integration

package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	hookhandlers "github.com/nextlevelbuilder/goclaw/internal/hooks/handlers"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestHooksE2E_AllEvents_AllowsDefault fires each of the 7 lifecycle events
// through the dispatcher with a single HTTP allow hook installed. Asserts
// the decision is allow and an audit row lands in hook_executions for every
// event, proving wire-up for all lifecycle points.
func TestHooksE2E_AllEvents_AllowsDefault(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	allowLoopbackForTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hs := pg.NewPGHookStore(db)

	events := []hooks.HookEvent{
		hooks.EventSessionStart,
		hooks.EventUserPromptSubmit,
		hooks.EventPreToolUse,
		hooks.EventPostToolUse,
		hooks.EventStop,
		hooks.EventSubagentStart,
		hooks.EventSubagentStop,
	}

	d := newDispatcher(t, hs, map[hooks.HandlerType]hooks.Handler{
		hooks.HandlerHTTP: &hookhandlers.HTTPHandler{Client: srv.Client()},
	})

	for _, ev := range events {
		ev := ev
		t.Run(string(ev), func(t *testing.T) {
			cfg := hooks.HookConfig{
				TenantID:    tenantID,
				AgentID:     &agentID,
				Scope:       hooks.ScopeAgent,
				Event:       ev,
				HandlerType: hooks.HandlerHTTP,
				Config:      map[string]any{"url": srv.URL},
				TimeoutMS:   5000,
				OnTimeout:   hooks.DecisionAllow,
				Enabled:     true,
				Version:     1,
				Source:      "api",
				Metadata:    map[string]any{},
			}
			hookID, err := hs.Create(ctx, cfg)
			if err != nil {
				t.Fatalf("Create hook for %s: %v", ev, err)
			}
			t.Cleanup(func() {
				db.Exec("DELETE FROM hook_executions WHERE hook_id = $1", hookID)
				db.Exec("DELETE FROM hooks WHERE id = $1", hookID)
			})

			r, err := d.Fire(ctx, hooks.Event{
				EventID: uuid.NewString(), TenantID: tenantID, AgentID: agentID, HookEvent: ev,
			})
			if err != nil {
				t.Fatalf("Fire %s: %v", ev, err)
			}
			if r.Decision != hooks.DecisionAllow {
				t.Errorf("%s: decision=%q, want allow", ev, r.Decision)
			}

			// Non-blocking events write audit from a goroutine; poll briefly
			// so the assertion doesn't race the writer.
			pollAuditCount(t, hookID, "allow", 1, 2*time.Second)
		})
	}
}

// TestHooksE2E_CommandHandler_BlockOnExitTwo covers the command handler
// sync-block path end-to-end through the dispatcher + PG audit writer.
// Uses Lite edition (command type is gated per C2).
func TestHooksE2E_CommandHandler_BlockOnExitTwo(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	hs := pg.NewPGHookStore(db)

	cfg := hooks.HookConfig{
		TenantID: tenantID, AgentID: &agentID,
		Scope: hooks.ScopeAgent, Event: hooks.EventPreToolUse,
		HandlerType: hooks.HandlerCommand,
		Config:      map[string]any{"command": "exit 2"},
		TimeoutMS:   5000, OnTimeout: hooks.DecisionBlock,
		Enabled: true, Version: 1, Source: "api",
		Metadata: map[string]any{},
	}
	hookID, err := hs.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM hook_executions WHERE hook_id = $1", hookID)
		db.Exec("DELETE FROM hooks WHERE id = $1", hookID)
	})

	d := newDispatcher(t, hs, map[hooks.HandlerType]hooks.Handler{
		hooks.HandlerCommand: &hookhandlers.CommandHandler{Edition: edition.Lite},
	})

	r, err := d.Fire(ctx, hooks.Event{
		EventID: uuid.NewString(), TenantID: tenantID, AgentID: agentID,
		HookEvent: hooks.EventPreToolUse,
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if r.Decision != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block (exit 2)", r.Decision)
	}
	assertHookAuditCount(t, db, hookID, "block", 1)
}

// TestHooksE2E_ContextUpdateInjection proves that an allow + additional_context
// path writes to audit and preserves the decision ordering. Uses HTTP handler
// returning additionalContext in JSON.
func TestHooksE2E_ContextUpdateInjection(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	allowLoopbackForTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"decision":"allow","additionalContext":"system reminder"}`))
	}))
	defer srv.Close()

	hs := pg.NewPGHookStore(db)

	cfg := hooks.HookConfig{
		TenantID: tenantID, AgentID: &agentID,
		Scope: hooks.ScopeAgent, Event: hooks.EventUserPromptSubmit,
		HandlerType: hooks.HandlerHTTP,
		Config:      map[string]any{"url": srv.URL},
		TimeoutMS:   5000, OnTimeout: hooks.DecisionAllow,
		Enabled: true, Version: 1, Source: "api",
		Metadata: map[string]any{},
	}
	hookID, err := hs.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM hook_executions WHERE hook_id = $1", hookID)
		db.Exec("DELETE FROM hooks WHERE id = $1", hookID)
	})

	d := newDispatcher(t, hs, map[hooks.HandlerType]hooks.Handler{
		hooks.HandlerHTTP: &hookhandlers.HTTPHandler{Client: srv.Client()},
	})
	r, err := d.Fire(ctx, hooks.Event{
		EventID: uuid.NewString(), TenantID: tenantID, AgentID: agentID,
		HookEvent: hooks.EventUserPromptSubmit,
	})
	if err != nil || r.Decision != hooks.DecisionAllow {
		t.Errorf("decision=%q err=%v, want allow+nil", r.Decision, err)
	}
	assertHookAuditCount(t, db, hookID, "allow", 1)
}

// TestHooksE2E_TenantIsolation confirms hooks from tenant A never fire for
// tenant B's events (cross-tenant leak protection).
func TestHooksE2E_TenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, agentA := seedTenantAgent(t, db)
	tenantB, agentB := seedTenantAgent(t, db)
	allowLoopbackForTest(t)

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hs := pg.NewPGHookStore(db)
	// Hook registered under tenantA only.
	cfg := hooks.HookConfig{
		TenantID: tenantA, AgentID: &agentA,
		Scope: hooks.ScopeAgent, Event: hooks.EventUserPromptSubmit,
		HandlerType: hooks.HandlerHTTP,
		Config:      map[string]any{"url": srv.URL},
		TimeoutMS:   5000, OnTimeout: hooks.DecisionAllow,
		Enabled: true, Version: 1, Source: "api",
		Metadata: map[string]any{},
	}
	hookID, err := hs.Create(tenantCtx(tenantA), cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM hook_executions WHERE hook_id = $1", hookID)
		db.Exec("DELETE FROM hooks WHERE id = $1", hookID)
	})

	d := newDispatcher(t, hs, map[hooks.HandlerType]hooks.Handler{
		hooks.HandlerHTTP: &hookhandlers.HTTPHandler{Client: srv.Client()},
	})

	// Fire on tenant B (should NOT hit hook).
	if _, err := d.Fire(tenantCtx(tenantB), hooks.Event{
		EventID: uuid.NewString(), TenantID: tenantB, AgentID: agentB,
		HookEvent: hooks.EventUserPromptSubmit,
	}); err != nil {
		t.Fatalf("Fire tenantB: %v", err)
	}
	if hits != 0 {
		t.Errorf("tenant B event hit tenant A hook: hits=%d", hits)
	}

	// Fire on tenant A (should hit).
	if _, err := d.Fire(tenantCtx(tenantA), hooks.Event{
		EventID: uuid.NewString(), TenantID: tenantA, AgentID: agentA,
		HookEvent: hooks.EventUserPromptSubmit,
	}); err != nil {
		t.Fatalf("Fire tenantA: %v", err)
	}
	if hits != 1 {
		t.Errorf("tenant A hook hits=%d, want 1", hits)
	}
}

// TestHooksE2E_EditionGate_CommandOnStandard verifies the edition gate blocks
// command handler execution on Standard edition (C2 mitigation).
func TestHooksE2E_EditionGate_CommandOnStandard(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	hs := pg.NewPGHookStore(db)
	cfg := hooks.HookConfig{
		TenantID: tenantID, AgentID: &agentID,
		Scope: hooks.ScopeAgent, Event: hooks.EventPreToolUse,
		HandlerType: hooks.HandlerCommand,
		Config:      map[string]any{"command": "exit 0"},
		TimeoutMS:   5000, OnTimeout: hooks.DecisionBlock,
		Enabled: true, Version: 1, Source: "api",
		Metadata: map[string]any{},
	}
	hookID, err := hs.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM hook_executions WHERE hook_id = $1", hookID)
		db.Exec("DELETE FROM hooks WHERE id = $1", hookID)
	})

	// Dispatcher with Standard edition command handler — must refuse to run.
	d := newDispatcher(t, hs, map[hooks.HandlerType]hooks.Handler{
		hooks.HandlerCommand: &hookhandlers.CommandHandler{Edition: edition.Standard},
	})
	r, _ := d.Fire(ctx, hooks.Event{
		EventID: uuid.NewString(), TenantID: tenantID, AgentID: agentID,
		HookEvent: hooks.EventPreToolUse,
	})
	// Blocking event + handler error → fail-closed Block.
	if r.Decision != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block (edition gate)", r.Decision)
	}
}

// assertHookAuditCount verifies the hook_executions row count for a given
// hook + decision. Helps tests confirm the audit writer ran.
func assertHookAuditCount(t *testing.T, _ any, hookID uuid.UUID, decision string, want int) {
	t.Helper()
	db := testDB(t)
	var got int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM hook_executions WHERE hook_id = $1 AND decision = $2`,
		hookID, decision,
	).Scan(&got); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if got != want {
		t.Errorf("audit rows (hook=%s decision=%s)=%d, want %d", hookID, decision, got, want)
	}
}

// pollAuditCount waits up to timeout for hook_executions to reach the target
// count — accommodates the dispatcher's async audit path for non-blocking events.
func pollAuditCount(t *testing.T, hookID uuid.UUID, decision string, want int, timeout time.Duration) {
	t.Helper()
	db := testDB(t)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var got int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM hook_executions WHERE hook_id = $1 AND decision = $2`,
			hookID, decision,
		).Scan(&got); err == nil && got >= want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Errorf("audit row count for hook=%s decision=%s did not reach %d within %v", hookID, decision, want, timeout)
}

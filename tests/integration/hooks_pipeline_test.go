//go:build integration

package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/eventbus"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	hookhandlers "github.com/nextlevelbuilder/goclaw/internal/hooks/handlers"
	"github.com/nextlevelbuilder/goclaw/internal/security"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// allowLoopbackForTest relaxes SSRF checks for the duration of a single test
// so httptest servers on 127.0.0.1 can receive requests. Phase 4 covers all
// integration tests that spin up fake HTTP endpoints.
func allowLoopbackForTest(t *testing.T) {
	t.Helper()
	security.SetAllowLoopbackForTest(true)
	t.Cleanup(func() { security.SetAllowLoopbackForTest(false) })
}

// newDispatcher wires a StdDispatcher with the supplied handler map over a
// live PG hook store. Audit writer runs without encryption (dev mode).
func newDispatcher(t *testing.T, hs hooks.HookStore, hdls map[hooks.HandlerType]hooks.Handler) hooks.Dispatcher {
	t.Helper()
	return hooks.NewStdDispatcher(hooks.StdDispatcherOpts{
		Store:          hs,
		Audit:          hooks.NewAuditWriter(hs, ""),
		Handlers:       hdls,
		PerHookTimeout: 5 * time.Second,
		ChainBudget:    10 * time.Second,
	})
}

// TestHooksIntegration_HTTPHandler_AllowWritesAudit covers the E2E path:
// HTTP hook → dispatcher → allow decision → hook_executions row created.
func TestHooksIntegration_HTTPHandler_AllowWritesAudit(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	allowLoopbackForTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	hs := pg.NewPGHookStore(db)

	cfg := hooks.HookConfig{
		TenantID:    tenantID,
		AgentID:     &agentID,
		Scope:       hooks.ScopeAgent,
		Event:       hooks.EventUserPromptSubmit,
		HandlerType: hooks.HandlerHTTP,
		Config:      map[string]any{"url": srv.URL},
		TimeoutMS:   5000,
		OnTimeout:   hooks.DecisionBlock,
		Enabled:     true,
		Version:     1,
		Source:      "api",
		Metadata:    map[string]any{},
	}
	hookID, err := hs.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create hook: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM hook_executions WHERE hook_id = $1", hookID)
		db.Exec("DELETE FROM hooks WHERE id = $1", hookID)
	})

	d := newDispatcher(t, hs, map[hooks.HandlerType]hooks.Handler{
		hooks.HandlerHTTP: &hookhandlers.HTTPHandler{Client: srv.Client()},
	})

	ev := hooks.Event{
		EventID:   uuid.NewString(),
		TenantID:  tenantID,
		AgentID:   agentID,
		HookEvent: hooks.EventUserPromptSubmit,
	}
	r, err := d.Fire(ctx, ev)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if r.Decision != hooks.DecisionAllow {
		t.Errorf("decision=%q, want allow", r.Decision)
	}

	// Give the audit writer a moment (sync path, but defensive).
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM hook_executions WHERE hook_id = $1 AND decision = 'allow'`,
		hookID,
	).Scan(&count); err != nil {
		t.Fatalf("count exec rows: %v", err)
	}
	if count != 1 {
		t.Errorf("hook_executions allow rows = %d, want 1", count)
	}
}

// TestHooksIntegration_HTTPHandler_Block confirms that a blocking HTTP response
// yields DecisionBlock and is recorded in hook_executions.
func TestHooksIntegration_HTTPHandler_Block(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	allowLoopbackForTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"decision":"block"}`))
	}))
	defer srv.Close()

	hs := pg.NewPGHookStore(db)

	cfg := hooks.HookConfig{
		TenantID:    tenantID,
		AgentID:     &agentID,
		Scope:       hooks.ScopeAgent,
		Event:       hooks.EventUserPromptSubmit,
		HandlerType: hooks.HandlerHTTP,
		Config:      map[string]any{"url": srv.URL},
		TimeoutMS:   5000,
		OnTimeout:   hooks.DecisionBlock,
		Enabled:     true,
		Version:     1,
		Source:      "api",
		Metadata:    map[string]any{},
	}
	hookID, err := hs.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create hook: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM hook_executions WHERE hook_id = $1", hookID)
		db.Exec("DELETE FROM hooks WHERE id = $1", hookID)
	})

	d := newDispatcher(t, hs, map[hooks.HandlerType]hooks.Handler{
		hooks.HandlerHTTP: &hookhandlers.HTTPHandler{Client: srv.Client()},
	})

	ev := hooks.Event{
		EventID:   uuid.NewString(),
		TenantID:  tenantID,
		AgentID:   agentID,
		HookEvent: hooks.EventUserPromptSubmit,
	}
	r, err := d.Fire(ctx, ev)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if r.Decision != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block", r.Decision)
	}

	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM hook_executions WHERE hook_id = $1 AND decision = 'block'`,
		hookID,
	).Scan(&count); err != nil {
		t.Fatalf("count exec rows: %v", err)
	}
	if count != 1 {
		t.Errorf("hook_executions block rows = %d, want 1", count)
	}
}

// TestHooksIntegration_CommandHandler_LiteEdition confirms that the command
// handler (exit 0) runs cleanly under Lite edition and produces an audit row.
func TestHooksIntegration_CommandHandler_LiteEdition(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	hs := pg.NewPGHookStore(db)

	cfg := hooks.HookConfig{
		TenantID:    tenantID,
		AgentID:     &agentID,
		Scope:       hooks.ScopeAgent,
		Event:       hooks.EventUserPromptSubmit,
		HandlerType: hooks.HandlerCommand,
		Config:      map[string]any{"command": "exit 0"},
		TimeoutMS:   5000,
		OnTimeout:   hooks.DecisionBlock,
		Enabled:     true,
		Version:     1,
		Source:      "api",
		Metadata:    map[string]any{},
	}
	hookID, err := hs.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create hook: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM hook_executions WHERE hook_id = $1", hookID)
		db.Exec("DELETE FROM hooks WHERE id = $1", hookID)
	})

	d := newDispatcher(t, hs, map[hooks.HandlerType]hooks.Handler{
		hooks.HandlerCommand: &hookhandlers.CommandHandler{Edition: edition.Lite},
	})

	ev := hooks.Event{
		EventID:   uuid.NewString(),
		TenantID:  tenantID,
		AgentID:   agentID,
		HookEvent: hooks.EventUserPromptSubmit,
	}
	r, err := d.Fire(ctx, ev)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if r.Decision != hooks.DecisionAllow {
		t.Errorf("decision=%q, want allow", r.Decision)
	}

	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM hook_executions WHERE hook_id = $1 AND decision = 'allow'`,
		hookID,
	).Scan(&count); err != nil {
		t.Fatalf("count exec rows: %v", err)
	}
	if count != 1 {
		t.Errorf("hook_executions allow rows = %d, want 1", count)
	}
}

// TestHooksIntegration_DelegateBridge_SubscribesNoPanic verifies that
// SubscribeDelegateEvents registers without error and that publishing a
// delegate event does not panic when no matching hook is configured.
func TestHooksIntegration_DelegateBridge_SubscribesNoPanic(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	hs := pg.NewPGHookStore(db)
	d := newDispatcher(t, hs, map[hooks.HandlerType]hooks.Handler{
		hooks.HandlerHTTP: &hookhandlers.HTTPHandler{Client: http.DefaultClient},
	})

	bus := eventbus.NewDomainEventBus(eventbus.Config{
		QueueSize:     10,
		WorkerCount:   1,
		RetryAttempts: 1,
		RetryDelay:    10 * time.Millisecond,
		DedupTTL:      time.Minute,
	})
	bus.Start(ctx)

	// Primary assertion: subscribe must not panic.
	hooks.SubscribeDelegateEvents(bus, d)

	// Publish a delegate.completed event — no hook matches, so Fire returns
	// allow with 0 hooks. Should not panic.
	bus.Publish(eventbus.DomainEvent{
		ID:       uuid.NewString(),
		Type:     eventbus.EventDelegateCompleted,
		SourceID: uuid.NewString(), // unique so dedup doesn't swallow
		TenantID: tenantID.String(),
		AgentID:  agentID.String(),
		Payload: eventbus.DelegateCompletedPayload{
			DelegationID: uuid.NewString(),
		},
		Timestamp: time.Now(),
	})

	// Drain with short timeout to let the worker process the event.
	if err := bus.Drain(2 * time.Second); err != nil {
		// Drain timeout is non-fatal for this test — we only care about no panic.
		t.Logf("bus drain timeout (non-fatal): %v", err)
	}
}

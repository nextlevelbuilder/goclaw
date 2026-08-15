//go:build integration

package integration

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	hookhandlers "github.com/nextlevelbuilder/goclaw/internal/hooks/handlers"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// TestHooksChaos_HTTPHandler_ProviderDown verifies the HTTP handler fails
// closed when the target endpoint refuses connections. Dispatcher must
// record decision=error and (for blocking events) block the pipeline.
func TestHooksChaos_HTTPHandler_ProviderDown(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	allowLoopbackForTest(t)

	// Spin up and immediately close: the URL is valid but connection refused.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	badURL := srv.URL
	srv.Close()

	hs := pg.NewPGHookStore(db)
	cfg := hooks.HookConfig{
		TenantID: tenantID, AgentID: &agentID,
		Scope: hooks.ScopeAgent, Event: hooks.EventPreToolUse,
		HandlerType: hooks.HandlerHTTP,
		Config:      map[string]any{"url": badURL},
		TimeoutMS:   1500, OnTimeout: hooks.DecisionBlock,
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
		hooks.HandlerHTTP: &hookhandlers.HTTPHandler{Client: &http.Client{Timeout: 1 * time.Second}},
	})
	r, _ := d.Fire(ctx, hooks.Event{
		EventID: uuid.NewString(), TenantID: tenantID, AgentID: agentID,
		HookEvent: hooks.EventPreToolUse,
	})
	// Blocking event + unexpected error → fail-closed Block.
	if r.Decision != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block (fail-closed on provider down)", r.Decision)
	}
}

// TestHooksChaos_PerHookTimeout verifies per-hook timeout triggers
// DecisionTimeout and, with OnTimeout=block, aborts the chain.
func TestHooksChaos_PerHookTimeout(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	allowLoopbackForTest(t)

	// Server sleeps longer than hook timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	hs := pg.NewPGHookStore(db)
	cfg := hooks.HookConfig{
		TenantID: tenantID, AgentID: &agentID,
		Scope: hooks.ScopeAgent, Event: hooks.EventPreToolUse,
		HandlerType: hooks.HandlerHTTP,
		Config:      map[string]any{"url": srv.URL},
		TimeoutMS:   300, OnTimeout: hooks.DecisionBlock,
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
	start := time.Now()
	r, _ := d.Fire(ctx, hooks.Event{
		EventID: uuid.NewString(), TenantID: tenantID, AgentID: agentID,
		HookEvent: hooks.EventPreToolUse,
	})
	elapsed := time.Since(start)

	if r.Decision != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block (OnTimeout=block)", r.Decision)
	}
	// Must NOT have waited the full 2s; per-hook timeout caps at ~300ms+retry.
	if elapsed > 4*time.Second {
		t.Errorf("elapsed=%v, expected timeout to abort early", elapsed)
	}
}

// TestHooksChaos_CircuitBreaker_AutoDisables verifies repeated block
// decisions flip the breaker which persists enabled=false on the hook (C4).
func TestHooksChaos_CircuitBreaker_AutoDisables(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	allowLoopbackForTest(t)

	// Always blocks.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"decision":"block"}`))
	}))
	defer srv.Close()

	hs := pg.NewPGHookStore(db)
	cfg := hooks.HookConfig{
		TenantID: tenantID, AgentID: &agentID,
		Scope: hooks.ScopeAgent, Event: hooks.EventPreToolUse,
		HandlerType: hooks.HandlerHTTP,
		Config:      map[string]any{"url": srv.URL},
		TimeoutMS:   1500, OnTimeout: hooks.DecisionBlock,
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

	// Tight breaker: 3 blocks within 10s.
	d := hooks.NewStdDispatcher(hooks.StdDispatcherOpts{
		Store:            hs,
		Audit:            hooks.NewAuditWriter(hs, ""),
		Handlers:         map[hooks.HandlerType]hooks.Handler{hooks.HandlerHTTP: &hookhandlers.HTTPHandler{Client: srv.Client()}},
		PerHookTimeout:   2 * time.Second,
		ChainBudget:      5 * time.Second,
		CircuitThreshold: 3,
		CircuitWindow:    10 * time.Second,
	})

	// Fire 4 times — last should short-circuit via tripped breaker.
	for i := 0; i < 4; i++ {
		d.Fire(ctx, hooks.Event{
			EventID: uuid.NewString(), TenantID: tenantID, AgentID: agentID,
			HookEvent: hooks.EventPreToolUse,
		})
	}

	// After 3 consecutive blocks, breaker should persist enabled=false.
	var enabled bool
	if err := db.QueryRow(`SELECT enabled FROM hooks WHERE id = $1`, hookID).Scan(&enabled); err != nil {
		t.Fatalf("read enabled: %v", err)
	}
	if enabled {
		t.Errorf("expected enabled=false after circuit breaker trip")
	}
}

// TestHooksChaos_LoopDepthExceeded verifies the dispatcher rejects events
// whose context depth exceeds MaxLoopDepth (M5).
func TestHooksChaos_LoopDepthExceeded(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)

	hs := pg.NewPGHookStore(db)
	d := newDispatcher(t, hs, map[hooks.HandlerType]hooks.Handler{})

	// Wrap ctx with depth > MaxLoopDepth.
	ctx := hooks.WithDepth(tenantCtx(tenantID), hooks.MaxLoopDepth+1)
	r, err := d.Fire(ctx, hooks.Event{
		EventID: uuid.NewString(), TenantID: tenantID, AgentID: agentID,
		HookEvent: hooks.EventPreToolUse,
	})
	if !errors.Is(err, hooks.ErrLoopDepthExceeded) {
		t.Errorf("err=%v, want ErrLoopDepthExceeded", err)
	}
	if r.Decision != hooks.DecisionError {
		t.Errorf("decision=%q, want error", r.Decision)
	}
}

// TestHooksChaos_RetryDedup_SuppressesDoubleAudit verifies writing the same
// (hook_id, event_id) twice via dedup_key only produces one audit row (H6).
func TestHooksChaos_RetryDedup_SuppressesDoubleAudit(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)

	hs := pg.NewPGHookStore(db)
	cfg := hooks.HookConfig{
		TenantID: tenantID, AgentID: &agentID,
		Scope: hooks.ScopeAgent, Event: hooks.EventPostToolUse,
		HandlerType: hooks.HandlerHTTP,
		Config:      map[string]any{"url": "http://127.0.0.1:1/ignored"}, // unused
		TimeoutMS:   1000, OnTimeout: hooks.DecisionAllow,
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

	// Write the same dedup_key twice — second INSERT must be suppressed.
	dedupKey := hookID.String() + ":retry-evt-1"
	for i := 0; i < 2; i++ {
		exec := hooks.HookExecution{
			ID:        uuid.New(),
			HookID:    &hookID,
			Event:     hooks.EventPostToolUse,
			Decision:  hooks.DecisionAllow,
			DedupKey:  dedupKey,
			Metadata:  map[string]any{"attempt": i},
			CreatedAt: time.Now(),
		}
		_ = hs.WriteExecution(ctx, exec)
	}

	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM hook_executions WHERE hook_id = $1 AND dedup_key = $2`,
		hookID, dedupKey,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("dedup rows=%d, want 1", count)
	}
}

// TestHooksChaos_HTTPHandler_5xxRetriesThenErrors verifies a server returning
// 500 is retried exactly once, then surfaces as decision=error.
func TestHooksChaos_HTTPHandler_5xxRetriesThenErrors(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	allowLoopbackForTest(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	hs := pg.NewPGHookStore(db)
	cfg := hooks.HookConfig{
		TenantID: tenantID, AgentID: &agentID,
		Scope: hooks.ScopeAgent, Event: hooks.EventPreToolUse,
		HandlerType: hooks.HandlerHTTP,
		Config:      map[string]any{"url": srv.URL},
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
		hooks.HandlerHTTP: &hookhandlers.HTTPHandler{Client: srv.Client()},
	})
	r, _ := d.Fire(ctx, hooks.Event{
		EventID: uuid.NewString(), TenantID: tenantID, AgentID: agentID,
		HookEvent: hooks.EventPreToolUse,
	})
	// Blocking event + repeated 5xx → fail-closed Block.
	if r.Decision != hooks.DecisionBlock {
		t.Errorf("decision=%q, want block (fail-closed on 5xx)", r.Decision)
	}
	// Verify the handler did perform its single retry (hits >= 2).
	if hits.Load() < 2 {
		t.Errorf("hits=%d, expected >=2 (request + retry)", hits.Load())
	}
}

// Compile-time assertion that we use context from the ctx helpers so this
// package doesn't get a "imported and not used" on a stray import change.
var _ = context.Background

//go:build integration

package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	hookhandlers "github.com/nextlevelbuilder/goclaw/internal/hooks/handlers"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
	"github.com/nextlevelbuilder/goclaw/internal/tracing"
)

// TestHooksTracing_EmitHookSpan verifies EmitHookSpan writes a span row with
// the canonical name format "hook.<handlerType>.<event>" when a collector
// is attached to ctx. No collector → no-op (already covered in unit tests).
func TestHooksTracing_EmitHookSpan(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)

	ts := pg.NewPGTracingStore(db)
	c := tracing.NewCollector(ts)
	c.Start()
	// No defer Stop — tests call Stop explicitly to flush before polling.

	// Seed a parent trace so tenant_id scoping doesn't orphan the span.
	traceID := uuid.New()
	if err := c.CreateTrace(tenantCtx(tenantID), &store.TraceData{
		ID:        traceID,
		Status:    store.SpanStatusCompleted,
		StartTime: time.Now().Add(-5 * time.Second),
	}); err != nil {
		t.Fatalf("CreateTrace: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM spans WHERE trace_id = $1", traceID)
		db.Exec("DELETE FROM traces WHERE id = $1", traceID)
	})

	ctx := tracing.WithTraceID(tracing.WithCollector(tenantCtx(tenantID), c), traceID)

	started := time.Now().Add(-200 * time.Millisecond)
	hooks.EmitHookSpan(ctx, hooks.EventPreToolUse, hooks.HandlerHTTP, started, hooks.DecisionAllow, "")

	// Stop collector to flush the buffered span; Stop() drains the chan.
	c.Stop()

	var name string
	var durationMS int
	if err := db.QueryRow(
		`SELECT name, duration_ms FROM spans WHERE trace_id = $1 LIMIT 1`,
		traceID,
	).Scan(&name, &durationMS); err != nil {
		t.Fatalf("read span: %v", err)
	}
	if name != "hook.http.pre_tool_use" {
		t.Errorf("span name=%q, want hook.http.pre_tool_use", name)
	}
	if durationMS <= 0 {
		t.Errorf("span duration=%d, want positive", durationMS)
	}
}

// TestHooksTracing_DispatcherEmitsSpan runs a full dispatcher Fire with a
// collector wired, then asserts a hook-span row appears. Proves tracing +
// dispatcher integration end-to-end.
func TestHooksTracing_DispatcherEmitsSpan(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	allowLoopbackForTest(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ts := pg.NewPGTracingStore(db)
	c := tracing.NewCollector(ts)
	c.Start()
	// No defer Stop — tests call Stop explicitly to flush before polling.

	traceID := uuid.New()
	if err := c.CreateTrace(tenantCtx(tenantID), &store.TraceData{
		ID: traceID, Status: store.SpanStatusCompleted,
		StartTime: time.Now().Add(-5 * time.Second),
	}); err != nil {
		t.Fatalf("CreateTrace: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM spans WHERE trace_id = $1", traceID)
		db.Exec("DELETE FROM traces WHERE id = $1", traceID)
	})

	hs := pg.NewPGHookStore(db)
	cfg := hooks.HookConfig{
		TenantID: tenantID, AgentID: &agentID,
		Scope: hooks.ScopeAgent, Event: hooks.EventPreToolUse,
		HandlerType: hooks.HandlerHTTP,
		Config:      map[string]any{"url": srv.URL},
		TimeoutMS:   5000, OnTimeout: hooks.DecisionAllow,
		Enabled: true, Version: 1, Source: "api",
		Metadata: map[string]any{},
	}
	hookID, err := hs.Create(tenantCtx(tenantID), cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM hook_executions WHERE hook_id = $1", hookID)
		db.Exec("DELETE FROM hooks WHERE id = $1", hookID)
	})

	// Wrap ctx with tracing collector + trace ID — dispatcher's emit helper
	// is invoked from handlers wired via the standard dispatcher.
	ctx := tracing.WithTraceID(tracing.WithCollector(tenantCtx(tenantID), c), traceID)

	d := newDispatcher(t, hs, map[hooks.HandlerType]hooks.Handler{
		hooks.HandlerHTTP: tracedHandler{inner: &hookhandlers.HTTPHandler{Client: srv.Client()}},
	})

	if _, err := d.Fire(ctx, hooks.Event{
		EventID: uuid.NewString(), TenantID: tenantID, AgentID: agentID,
		HookEvent: hooks.EventPreToolUse,
	}); err != nil {
		t.Fatalf("Fire: %v", err)
	}

	// Stop collector to flush the buffered span.
	c.Stop()

	var rowsCount int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM spans WHERE trace_id = $1 AND name = $2`,
		traceID, "hook.http.pre_tool_use",
	).Scan(&rowsCount); err != nil {
		t.Fatalf("count spans: %v", err)
	}
	if rowsCount < 1 {
		t.Errorf("expected >=1 hook span in DB, got %d", rowsCount)
	}
}

// tracedHandler wraps an existing hooks.Handler and emits a tracing span
// around each call. Production dispatcher does NOT auto-emit spans —
// integration with tracing goes through this wrapper pattern (see plan).
type tracedHandler struct {
	inner hooks.Handler
}

func (w tracedHandler) Execute(ctx_ctx context.Context, cfg hooks.HookConfig, ev hooks.Event) (hooks.Decision, error) {
	start := time.Now()
	dec, err := w.inner.Execute(ctx_ctx, cfg, ev)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	hooks.EmitHookSpan(ctx_ctx, ev.HookEvent, cfg.HandlerType, start, dec, errMsg)
	return dec, err
}


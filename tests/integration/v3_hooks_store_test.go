//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// newHookStore returns a live PG-backed HookStore for integration coverage.
func newHookStore(t *testing.T) hooks.HookStore {
	t.Helper()
	db := testDB(t)
	return pg.NewPGHookStore(db)
}

// TestHooksStore_CRUDRoundTrip exercises the full CRUD path against a real PG
// instance — mirrors the in-package unit test but with the migration-applied
// schema to catch any drift between schema.sql statements and DDL migrations.
func TestHooksStore_CRUDRoundTrip(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newHookStore(t)

	cfg := hooks.HookConfig{
		TenantID:    tenantID,
		AgentID:     &agentID,
		Scope:       hooks.ScopeAgent,
		Event:       hooks.EventPreToolUse,
		HandlerType: hooks.HandlerHTTP,
		Config:      map[string]any{"url": "https://example.test/hook"},
		TimeoutMS:   5000,
		OnTimeout:   hooks.DecisionBlock,
		Priority:    10,
		Enabled:     true,
		Version:     1,
		Source:      "ui",
		Metadata:    map[string]any{"notes": "it"},
	}
	id, err := s.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM hook_executions WHERE hook_id = $1", id)
		db.Exec("DELETE FROM hooks WHERE id = $1", id)
	})

	got, err := s.GetByID(ctx, id)
	if err != nil || got == nil {
		t.Fatalf("GetByID: id=%s err=%v got=%v", id, err, got)
	}
	if got.Event != hooks.EventPreToolUse || got.HandlerType != hooks.HandlerHTTP {
		t.Errorf("roundtrip mismatch: event=%q handler=%q", got.Event, got.HandlerType)
	}

	// Update bumps version and patches priority.
	if err := s.Update(ctx, id, map[string]any{"priority": 42}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got2, _ := s.GetByID(ctx, id)
	if got2.Priority != 42 {
		t.Errorf("priority after update = %d, want 42", got2.Priority)
	}
	if got2.Version <= got.Version {
		t.Errorf("version after update = %d, want > %d", got2.Version, got.Version)
	}

	// ResolveForEvent returns the agent-scoped hook.
	resolved, err := s.ResolveForEvent(ctx, hooks.Event{
		TenantID:  tenantID,
		AgentID:   agentID,
		HookEvent: hooks.EventPreToolUse,
	})
	if err != nil {
		t.Fatalf("ResolveForEvent: %v", err)
	}
	found := false
	for _, h := range resolved {
		if h.ID == id {
			found = true
		}
	}
	if !found {
		t.Errorf("ResolveForEvent missing hook id %s (resolved=%d)", id, len(resolved))
	}
}

// TestHooksStore_TenantIsolation confirms the store refuses to hand rows from
// one tenant to another. Regression guard for the tenant-scope WHERE clauses.
func TestHooksStore_TenantIsolation(t *testing.T) {
	db := testDB(t)
	tenantA, agentA := seedTenantAgent(t, db)
	tenantB, _ := seedTenantAgent(t, db)
	s := newHookStore(t)

	cfgA := hooks.HookConfig{
		TenantID:    tenantA,
		AgentID:     &agentA,
		Scope:       hooks.ScopeAgent,
		Event:       hooks.EventPreToolUse,
		HandlerType: hooks.HandlerHTTP,
		Config:      map[string]any{},
		TimeoutMS:   5000,
		OnTimeout:   hooks.DecisionBlock,
		Enabled:     true,
		Version:     1,
		Source:      "ui",
		Metadata:    map[string]any{},
	}
	id, err := s.Create(tenantCtx(tenantA), cfgA)
	if err != nil {
		t.Fatalf("Create tenantA hook: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM hooks WHERE id = $1", id)
	})

	// Tenant B must not see A's row.
	got, err := s.GetByID(tenantCtx(tenantB), id)
	if err != nil {
		t.Fatalf("GetByID (tenantB): %v", err)
	}
	if got != nil {
		t.Errorf("tenantB saw tenantA's hook (id=%s) — tenant isolation broken", id)
	}

	// Cross-tenant context (master scope) can see it.
	got2, err := s.GetByID(crossTenantCtx(), id)
	if err != nil {
		t.Fatalf("GetByID (master): %v", err)
	}
	if got2 == nil {
		t.Error("master-scope read returned nil for existing hook")
	}
}

// TestHooksStore_DedupKeyIdempotent proves the unique index on
// hook_executions.dedup_key makes duplicate WriteExecution calls a no-op.
func TestHooksStore_DedupKeyIdempotent(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	s := newHookStore(t)

	cfg := hooks.HookConfig{
		TenantID:    tenantID,
		AgentID:     &agentID,
		Scope:       hooks.ScopeAgent,
		Event:       hooks.EventPreToolUse,
		HandlerType: hooks.HandlerHTTP,
		Config:      map[string]any{},
		TimeoutMS:   5000,
		OnTimeout:   hooks.DecisionBlock,
		Enabled:     true,
		Version:     1,
		Source:      "ui",
		Metadata:    map[string]any{},
	}
	hookID, err := s.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM hook_executions WHERE hook_id = $1", hookID)
		db.Exec("DELETE FROM hooks WHERE id = $1", hookID)
	})

	exec := hooks.HookExecution{
		ID:       uuid.New(),
		HookID:   &hookID,
		Event:    hooks.EventPreToolUse,
		Decision: hooks.DecisionAllow,
		DedupKey: hookID.String() + ":evt-once",
		Metadata: map[string]any{},
	}
	if err := s.WriteExecution(ctx, exec); err != nil {
		t.Fatalf("first WriteExecution: %v", err)
	}
	// Second write with same dedup_key must be silently accepted.
	exec.ID = uuid.New()
	if err := s.WriteExecution(ctx, exec); err != nil {
		t.Fatalf("second WriteExecution (duplicate dedup_key): %v", err)
	}
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM hook_executions WHERE dedup_key = $1`,
		exec.DedupKey,
	).Scan(&count); err != nil {
		t.Fatalf("count exec rows: %v", err)
	}
	if count != 1 {
		t.Errorf("hook_executions rows for dedup_key = %d, want 1", count)
	}
}

// TestHooksStore_CanonicalInputHashDeterministic — input_hash is a pure
// function of (tool_name, args); this test guards against someone accidentally
// rewriting it in a way that makes it locale/order-dependent at the boundary.
func TestHooksStore_CanonicalInputHashDeterministic(t *testing.T) {
	// Same map, different insertion orders must hash identically.
	a, err := hooks.CanonicalInputHash("Edit",
		map[string]any{"file": "a.go", "line": 10, "tags": []any{"go", "fmt"}})
	if err != nil {
		t.Fatalf("hash a: %v", err)
	}
	b, err := hooks.CanonicalInputHash("Edit",
		map[string]any{"tags": []any{"go", "fmt"}, "line": 10, "file": "a.go"})
	if err != nil {
		t.Fatalf("hash b: %v", err)
	}
	if a != b {
		t.Errorf("canonical hash changed across key orderings: %s vs %s", a, b)
	}
}

// ensure test compiles against the live store package even if a refactor moves
// context helpers around.
var _ context.Context = tenantCtx(uuid.Nil)
var _ = store.MasterTenantID

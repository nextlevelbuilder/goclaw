//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ─── test setup ──────────────────────────────────────────────────────────────

func newHookTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "hooks_test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	if err := EnsureSchema(db); err != nil {
		db.Close()
		t.Fatalf("EnsureSchema: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedHookTenantAgent inserts a minimal tenant + agent for FK satisfaction.
func seedHookTenantAgent(t *testing.T, db *sql.DB) (tenantID, agentID uuid.UUID) {
	t.Helper()
	tenantID = uuid.Must(uuid.NewV7())
	agentID = uuid.Must(uuid.NewV7())

	_, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES (?,?,?,'active')`,
		tenantID.String(), "hook-test-"+tenantID.String()[:8], "ht"+tenantID.String()[:8])
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES (?,?,?,'predefined','active','test','test-model','owner')`,
		agentID.String(), tenantID.String(), "ha-"+agentID.String()[:8])
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	return tenantID, agentID
}

func sqliteTenantCtx(tenantID uuid.UUID) context.Context {
	return store.WithTenantID(context.Background(), tenantID)
}

func sqliteMasterCtx() context.Context {
	return store.WithTenantID(context.Background(), store.MasterTenantID)
}

func sqliteMinimalHook(tenantID uuid.UUID, event hooks.HookEvent) hooks.HookConfig {
	return hooks.HookConfig{
		TenantID:    tenantID,
		Event:       event,
		HandlerType: hooks.HandlerCommand,
		Scope:       hooks.ScopeTenant,
		Config:      map[string]any{"cmd": "echo ok"},
		Metadata:    map[string]any{},
		TimeoutMS:   5000,
		OnTimeout:   hooks.DecisionBlock,
		Source:      "api",
		Enabled:     true,
		Priority:    0,
	}
}

// ─── CRUD ────────────────────────────────────────────────────────────────────

func TestSQLiteHookStore_CRUD(t *testing.T) {
	db := newHookTestDB(t)
	tenantID, _ := seedHookTenantAgent(t, db)
	s := NewSQLiteHookStore(db)
	ctx := sqliteTenantCtx(tenantID)

	// Create
	cfg := sqliteMinimalHook(tenantID, hooks.EventPreToolUse)
	id, err := s.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id == uuid.Nil {
		t.Fatal("Create returned nil UUID")
	}

	// GetByID
	got, err := s.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got == nil {
		t.Fatal("GetByID returned nil for existing hook")
	}
	if got.Event != hooks.EventPreToolUse {
		t.Errorf("event mismatch: got %q want %q", got.Event, hooks.EventPreToolUse)
	}
	if got.TenantID != tenantID {
		t.Errorf("tenant_id mismatch: got %s want %s", got.TenantID, tenantID)
	}
	if got.Version != 1 {
		t.Errorf("initial version should be 1, got %d", got.Version)
	}
	if !got.Enabled {
		t.Error("hook should be enabled")
	}
	if len(got.Config) == 0 {
		t.Error("config should not be empty")
	}

	// GetByID — not found returns (nil, nil)
	missing, err := s.GetByID(ctx, uuid.Must(uuid.NewV7()))
	if err != nil {
		t.Fatalf("GetByID(missing): unexpected error %v", err)
	}
	if missing != nil {
		t.Fatal("GetByID(missing): expected nil")
	}

	// Update — bumps version
	if err := s.Update(ctx, id, map[string]any{"priority": 10}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	updated, err := s.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}
	if updated.Priority != 10 {
		t.Errorf("priority not updated: got %d want 10", updated.Priority)
	}
	if updated.Version != 2 {
		t.Errorf("version should be 2 after update, got %d", updated.Version)
	}

	// Update — reject 'version' key
	if err := s.Update(ctx, id, map[string]any{"version": 99}); err == nil {
		t.Fatal("Update with 'version' key should return error")
	}

	// Delete
	if err := s.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	afterDelete, err := s.GetByID(ctx, id)
	if err != nil || afterDelete != nil {
		t.Fatalf("GetByID after Delete: want (nil,nil), got (%v,%v)", afterDelete, err)
	}
}

// ─── Tenant isolation ─────────────────────────────────────────────────────────

func TestSQLiteHookStore_TenantIsolation(t *testing.T) {
	db := newHookTestDB(t)
	tenantA, _ := seedHookTenantAgent(t, db)
	tenantB, _ := seedHookTenantAgent(t, db)
	s := NewSQLiteHookStore(db)

	ctxA := sqliteTenantCtx(tenantA)
	ctxB := sqliteTenantCtx(tenantB)

	// Create hook for tenant A.
	idA, err := s.Create(ctxA, sqliteMinimalHook(tenantA, hooks.EventStop))
	if err != nil {
		t.Fatalf("Create A: %v", err)
	}

	got, err := s.GetByID(ctxB, idA)
	if err != nil {
		t.Fatalf("GetByID B: %v", err)
	}
	if got != nil {
		t.Errorf("tenant B saw tenant A hook %s in GetByID", idA)
	}

	gotMaster, err := s.GetByID(sqliteMasterCtx(), idA)
	if err != nil {
		t.Fatalf("GetByID master: %v", err)
	}
	if gotMaster == nil {
		t.Fatal("master scope should see tenant A hook in GetByID")
	}

	// List from tenant B must not include tenant A's hook.
	listB, err := s.List(ctxB, hooks.ListFilter{})
	if err != nil {
		t.Fatalf("List B: %v", err)
	}
	for _, h := range listB {
		if h.ID == idA {
			t.Errorf("tenant B saw tenant A hook %s", idA)
		}
	}

	// List from tenant A must include their own hook.
	listA, err := s.List(ctxA, hooks.ListFilter{})
	if err != nil {
		t.Fatalf("List A: %v", err)
	}
	found := false
	for _, h := range listA {
		if h.ID == idA {
			found = true
		}
	}
	if !found {
		t.Error("tenant A did not see their own hook in List")
	}

	// Tenant B cannot delete tenant A's hook.
	if err := s.Delete(ctxB, idA); err == nil {
		t.Error("tenant B should not be able to delete tenant A's hook")
	}

	// Cleanup.
	_ = s.Delete(ctxA, idA)
}

// ─── Partial unique indexes ───────────────────────────────────────────────────

// H9 (Phase 03): Create honors caller-supplied cfg.ID so the builtin seeder's
// idempotent UUIDv5 keys survive restarts and tests get deterministic IDs.
func TestSQLiteHookStore_CreateHonorsFixedID(t *testing.T) {
	db := newHookTestDB(t)
	tenantID, _ := seedHookTenantAgent(t, db)
	s := NewSQLiteHookStore(db)
	ctx := sqliteTenantCtx(tenantID)

	fixed := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	cfg := sqliteMinimalHook(tenantID, hooks.EventUserPromptSubmit)
	cfg.ID = fixed
	cfg.Scope = hooks.ScopeTenant

	got, err := s.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { s.Delete(sqliteMasterCtx(), got) })
	if got != fixed {
		t.Fatalf("Create returned id=%s, want %s (H9: caller id must be honored)", got, fixed)
	}

	// A nil cfg.ID still auto-generates.
	cfg2 := sqliteMinimalHook(tenantID, hooks.EventPreToolUse)
	cfg2.ID = uuid.Nil
	auto, err := s.Create(ctx, cfg2)
	if err != nil {
		t.Fatalf("Create auto: %v", err)
	}
	t.Cleanup(func() { s.Delete(sqliteMasterCtx(), auto) })
	if auto == uuid.Nil {
		t.Fatal("Create returned nil id for cfg.ID=uuid.Nil path")
	}
}

// ─── ResolveForEvent ordering ────────────────────────────────────────────────

func TestSQLiteHookStore_ResolveForEvent(t *testing.T) {
	db := newHookTestDB(t)
	tenantID, agentID := seedHookTenantAgent(t, db)
	s := NewSQLiteHookStore(db)
	ctx := sqliteTenantCtx(tenantID)

	// Insert two enabled hooks at different priorities.
	lo := sqliteMinimalHook(tenantID, hooks.EventPreToolUse)
	lo.Priority = 0
	hi := sqliteMinimalHook(tenantID, hooks.EventPreToolUse)
	hi.Priority = 20
	// Make hi a different handler_type to avoid unique index conflict.
	hi.HandlerType = hooks.HandlerHTTP

	idLo, _ := s.Create(ctx, lo)
	idHi, _ := s.Create(ctx, hi)
	t.Cleanup(func() {
		s.Delete(sqliteMasterCtx(), idLo)
		s.Delete(sqliteMasterCtx(), idHi)
	})

	event := hooks.Event{
		TenantID:  tenantID,
		AgentID:   agentID,
		HookEvent: hooks.EventPreToolUse,
	}
	resolved, err := s.ResolveForEvent(ctx, event)
	if err != nil {
		t.Fatalf("ResolveForEvent: %v", err)
	}
	if len(resolved) < 2 {
		t.Fatalf("expected >=2 resolved hooks, got %d", len(resolved))
	}
	// Highest priority first.
	if resolved[0].Priority < resolved[1].Priority {
		t.Errorf("wrong order: [0].priority=%d < [1].priority=%d",
			resolved[0].Priority, resolved[1].Priority)
	}

	// Disabled hook must not appear.
	dis := sqliteMinimalHook(tenantID, hooks.EventPreToolUse)
	dis.Enabled = false
	dis.HandlerType = hooks.HandlerPrompt // third distinct handler_type
	idDis, _ := s.Create(ctx, dis)
	t.Cleanup(func() { s.Delete(sqliteMasterCtx(), idDis) })

	resolved2, _ := s.ResolveForEvent(ctx, event)
	for _, h := range resolved2 {
		if h.ID == idDis {
			t.Error("disabled hook appeared in ResolveForEvent result")
		}
	}
}

// ─── WriteExecution dedup ────────────────────────────────────────────────────

func TestSQLiteHookStore_WriteExecution(t *testing.T) {
	db := newHookTestDB(t)
	tenantID, _ := seedHookTenantAgent(t, db)
	s := NewSQLiteHookStore(db)
	ctx := sqliteTenantCtx(tenantID)

	// Create a parent hook for FK.
	hookID, err := s.Create(ctx, sqliteMinimalHook(tenantID, hooks.EventStop))
	if err != nil {
		t.Fatalf("Create hook: %v", err)
	}

	execID := uuid.Must(uuid.NewV7())
	dedup := "sqlite-dedup-" + execID.String()[:8]
	exec := hooks.HookExecution{
		ID:         execID,
		HookID:     &hookID,
		SessionID:  "sess-sqlite-001",
		Event:      hooks.EventStop,
		InputHash:  "deadbeef",
		Decision:   hooks.DecisionAllow,
		DurationMS: 7,
		DedupKey:   dedup,
		Metadata:   map[string]any{"src": "test"},
		CreatedAt:  time.Now().UTC(),
	}

	// First insert must succeed.
	if err := s.WriteExecution(ctx, exec); err != nil {
		t.Fatalf("WriteExecution: %v", err)
	}

	// Duplicate dedup_key must be silently ignored.
	if err := s.WriteExecution(ctx, exec); err != nil {
		t.Fatalf("WriteExecution (dedup): %v", err)
	}

	// Verify exactly one row.
	var count int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM hook_executions WHERE dedup_key=?", dedup,
	).Scan(&count); err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 execution row, got %d", count)
	}
}

// ─── Cache invalidation ───────────────────────────────────────────────────────

func TestSQLiteHookStore_CacheInvalidatedOnWrite(t *testing.T) {
	db := newHookTestDB(t)
	tenantID, agentID := seedHookTenantAgent(t, db)
	s := NewSQLiteHookStore(db)
	ctx := sqliteTenantCtx(tenantID)

	event := hooks.Event{
		TenantID:  tenantID,
		AgentID:   agentID,
		HookEvent: hooks.EventSessionStart,
	}

	// Warm cache.
	before, err := s.ResolveForEvent(ctx, event)
	if err != nil {
		t.Fatalf("ResolveForEvent: %v", err)
	}
	beforeCount := len(before)

	// Create a new hook — must invalidate cache.
	id, err := s.Create(ctx, sqliteMinimalHook(tenantID, hooks.EventSessionStart))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { s.Delete(sqliteMasterCtx(), id) })

	after, err := s.ResolveForEvent(ctx, event)
	if err != nil {
		t.Fatalf("ResolveForEvent after create: %v", err)
	}
	if len(after) <= beforeCount {
		t.Errorf("cache not invalidated: before=%d after=%d", beforeCount, len(after))
	}
}

// ─── Global-scope hooks visible to all tenants ────────────────────────────────

func TestSQLiteHookStore_GlobalScopeVisibleToTenant(t *testing.T) {
	db := newHookTestDB(t)
	tenantID, agentID := seedHookTenantAgent(t, db)
	s := NewSQLiteHookStore(db)

	// Create a global hook under MasterTenantID.
	globalCfg := hooks.HookConfig{
		TenantID:    store.MasterTenantID,
		Event:       hooks.EventPostToolUse,
		HandlerType: hooks.HandlerCommand,
		Scope:       hooks.ScopeGlobal,
		Config:      map[string]any{"cmd": "audit.sh"},
		Metadata:    map[string]any{},
		TimeoutMS:   3000,
		OnTimeout:   hooks.DecisionAllow,
		Source:      "seed",
		Enabled:     true,
		Priority:    5,
	}
	globalID, err := s.Create(sqliteMasterCtx(), globalCfg)
	if err != nil {
		t.Fatalf("Create global hook: %v", err)
	}
	t.Cleanup(func() { s.Delete(sqliteMasterCtx(), globalID) })

	// ResolveForEvent from tenant scope must include the global hook.
	event := hooks.Event{
		TenantID:  tenantID,
		AgentID:   agentID,
		HookEvent: hooks.EventPostToolUse,
	}
	resolved, err := s.ResolveForEvent(sqliteTenantCtx(tenantID), event)
	if err != nil {
		t.Fatalf("ResolveForEvent: %v", err)
	}
	found := false
	for _, h := range resolved {
		if h.ID == globalID {
			found = true
		}
	}
	if !found {
		t.Error("global hook not visible in ResolveForEvent from tenant scope")
	}
}

// TestSQLiteHookStore_BuiltinReadOnly mirrors the PG test: user-facing writes
// on source='builtin' rows may only toggle enabled; WithSeedBypass unlocks.
func TestSQLiteHookStore_BuiltinReadOnly(t *testing.T) {
	db := newHookTestDB(t)
	tenantID, _ := seedHookTenantAgent(t, db)
	s := NewSQLiteHookStore(db)

	seedCtx := hooks.WithSeedBypass(store.WithRole(sqliteMasterCtx(), store.RoleOwner))

	cfg := hooks.HookConfig{
		ID:          uuid.MustParse("11111111-2222-3333-4444-555555555555"),
		TenantID:    hooks.SentinelTenantID,
		Event:       hooks.EventUserPromptSubmit,
		HandlerType: hooks.HandlerScript,
		Scope:       hooks.ScopeGlobal,
		Config:      map[string]any{"source": "// v1"},
		Metadata:    map[string]any{"builtin": true, "version": 1},
		TimeoutMS:   500,
		OnTimeout:   hooks.DecisionAllow,
		Source:      hooks.SourceBuiltin,
		Enabled:     true,
	}
	id, err := s.Create(seedCtx, cfg)
	if err != nil {
		t.Fatalf("seed create: %v", err)
	}
	t.Cleanup(func() { s.Delete(seedCtx, id) })

	userCtx := sqliteTenantCtx(tenantID)

	if err := s.Update(userCtx, id, map[string]any{"matcher": "evil"}); !errors.Is(err, hooks.ErrBuiltinReadOnly) {
		t.Fatalf("Update(matcher) err=%v, want ErrBuiltinReadOnly", err)
	}

	if err := s.Update(sqliteMasterCtx(), id, map[string]any{"enabled": false}); err != nil {
		t.Fatalf("Update(enabled) should succeed: %v", err)
	}

	if err := s.Delete(userCtx, id); !errors.Is(err, hooks.ErrBuiltinReadOnly) {
		t.Fatalf("Delete user err=%v, want ErrBuiltinReadOnly", err)
	}

	if err := s.Update(seedCtx, id, map[string]any{"matcher": "ok"}); err != nil {
		t.Fatalf("seed-bypass Update should succeed: %v", err)
	}
}

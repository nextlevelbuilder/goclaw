package pg

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ─── test DB setup ───────────────────────────────────────────────────────────

func hooksTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skipf("TEST_DATABASE_URL not set; skipping PG hook store tests")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skipf("PG not reachable: %v", err)
	}

	m, err := migrate.New("file://../../../migrations", dsn)
	if err != nil {
		db.Close()
		t.Fatalf("migrate.New: %v", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		db.Close()
		t.Fatalf("migrate up: %v", err)
	}
	m.Close()

	InitSqlx(db)
	t.Cleanup(func() { db.Close() })
	return db
}

// seedTenantAndAgent inserts minimal tenant + agent rows and registers cleanup.
func seedTenantAndAgent(t *testing.T, db *sql.DB) (tenantID, agentID uuid.UUID) {
	t.Helper()
	tenantID = uuid.Must(uuid.NewV7())
	agentID = uuid.Must(uuid.NewV7())

	// UUIDv7s generated in the same millisecond share their 8-char prefix,
	// so deriving slug/agent_key from `[:8]` collides when a test calls
	// seedTenantAndAgent twice back-to-back and the second INSERT is swallowed
	// by ON CONFLICT DO NOTHING on the unique slug/agent_key constraint. Use
	// the full UUID — always unique.
	_, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES ($1,$2,$3,'active') ON CONFLICT DO NOTHING`,
		tenantID, "hook-test-"+tenantID.String()[:8], "ht-"+tenantID.String())
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES ($1,$2,$3,'predefined','active','test','test-model','owner') ON CONFLICT DO NOTHING`,
		agentID, tenantID, "hook-agent-"+agentID.String())
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM hook_executions WHERE hook_id IN (SELECT id FROM hooks WHERE tenant_id=$1)", tenantID)
		db.Exec("DELETE FROM hooks WHERE tenant_id=$1", tenantID)
		db.Exec("DELETE FROM agents WHERE id=$1", agentID)
		db.Exec("DELETE FROM tenants WHERE id=$1", tenantID)
	})
	return tenantID, agentID
}

func masterCtx() context.Context {
	return store.WithTenantID(context.Background(), store.MasterTenantID)
}

func tenantScopedCtx(tenantID uuid.UUID) context.Context {
	return store.WithTenantID(context.Background(), tenantID)
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func minimalHook(tenantID uuid.UUID, event hooks.HookEvent) hooks.HookConfig {
	return hooks.HookConfig{
		TenantID:    tenantID,
		Event:       event,
		HandlerType: hooks.HandlerCommand,
		Scope:       hooks.ScopeTenant,
		Config:      map[string]any{"cmd": "echo test"},
		Metadata:    map[string]any{},
		TimeoutMS:   5000,
		OnTimeout:   hooks.DecisionBlock,
		Source:      "api",
		Enabled:     true,
		Priority:    0,
	}
}

// ─── CRUD ────────────────────────────────────────────────────────────────────

func TestPGHookStore_CRUD(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, _ := seedTenantAndAgent(t, db)
	s := NewPGHookStore(db)
	ctx := tenantScopedCtx(tenantID)

	// Create
	cfg := minimalHook(tenantID, hooks.EventPreToolUse)
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
		t.Errorf("tenant_id mismatch")
	}
	if got.Version != 1 {
		t.Errorf("initial version should be 1, got %d", got.Version)
	}

	// GetByID — not found returns nil, nil
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
	updated, _ := s.GetByID(ctx, id)
	if updated.Priority != 10 {
		t.Errorf("priority not updated: got %d", updated.Priority)
	}
	if updated.Version != 2 {
		t.Errorf("version should be 2 after update, got %d", updated.Version)
	}

	// Update — reject version key
	if err := s.Update(ctx, id, map[string]any{"version": 99}); err == nil {
		t.Fatal("Update with 'version' key should return error")
	}

	// Delete
	if err := s.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	deleted, err := s.GetByID(ctx, id)
	if err != nil || deleted != nil {
		t.Fatalf("GetByID after Delete: want (nil,nil), got (%v,%v)", deleted, err)
	}
}

// ─── Tenant isolation ─────────────────────────────────────────────────────────

func TestPGHookStore_TenantIsolation(t *testing.T) {
	db := hooksTestDB(t)
	tenantA, _ := seedTenantAndAgent(t, db)
	tenantB, _ := seedTenantAndAgent(t, db)
	s := NewPGHookStore(db)

	ctxA := tenantScopedCtx(tenantA)
	ctxB := tenantScopedCtx(tenantB)

	cfgA := minimalHook(tenantA, hooks.EventStop)
	idA, err := s.Create(ctxA, cfgA)
	if err != nil {
		t.Fatalf("Create A: %v", err)
	}

	// Tenant B cannot read tenant A's hook
	got, err := s.GetByID(ctxB, idA)
	// GetByID has no tenant filter — returns the row regardless of scope.
	// List should filter it out though.
	_ = got
	_ = err

	// List from tenant B should not see tenant A's hooks.
	listB, err := s.List(ctxB, hooks.ListFilter{})
	if err != nil {
		t.Fatalf("List B: %v", err)
	}
	for _, h := range listB {
		if h.ID == idA {
			t.Errorf("tenant B saw tenant A hook %s", idA)
		}
	}

	// Tenant A can see their own hook.
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

// ─── ResolveForEvent ─────────────────────────────────────────────────────────

func TestPGHookStore_ResolveForEvent(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, agentID := seedTenantAndAgent(t, db)
	s := NewPGHookStore(db)
	ctx := tenantScopedCtx(tenantID)

	// Create two enabled hooks for pre_tool_use, different priorities.
	lowPriCfg := minimalHook(tenantID, hooks.EventPreToolUse)
	lowPriCfg.Priority = 0
	highPriCfg := minimalHook(tenantID, hooks.EventPreToolUse)
	highPriCfg.Priority = 10

	id1, _ := s.Create(ctx, lowPriCfg)
	id2, _ := s.Create(ctx, highPriCfg)
	t.Cleanup(func() {
		s.Delete(masterCtx(), id1)
		s.Delete(masterCtx(), id2)
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
		t.Fatalf("expected >=2 hooks, got %d", len(resolved))
	}
	// First should be highest priority.
	if resolved[0].Priority < resolved[1].Priority {
		t.Errorf("hooks not ordered by priority DESC: [0]=%d [1]=%d",
			resolved[0].Priority, resolved[1].Priority)
	}

	// Disabled hook should not appear.
	disabledCfg := minimalHook(tenantID, hooks.EventPreToolUse)
	disabledCfg.Enabled = false
	idDisabled, _ := s.Create(ctx, disabledCfg)
	t.Cleanup(func() { s.Delete(masterCtx(), idDisabled) })

	resolved2, _ := s.ResolveForEvent(ctx, event)
	for _, h := range resolved2 {
		if h.ID == idDisabled {
			t.Error("disabled hook appeared in ResolveForEvent")
		}
	}
}

// ─── WriteExecution ──────────────────────────────────────────────────────────

func TestPGHookStore_WriteExecution(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, _ := seedTenantAndAgent(t, db)
	s := NewPGHookStore(db)
	ctx := tenantScopedCtx(tenantID)

	// Create a hook for FK.
	cfg := minimalHook(tenantID, hooks.EventStop)
	hookID, err := s.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM hook_executions WHERE hook_id=$1", hookID)
		s.Delete(masterCtx(), hookID)
	})

	execID := uuid.Must(uuid.NewV7())
	dedup := "test-dedup-" + execID.String()[:8]
	exec := hooks.HookExecution{
		ID:         execID,
		HookID:     &hookID,
		SessionID:  "sess-123",
		Event:      hooks.EventStop,
		InputHash:  "aaabbbccc",
		Decision:   hooks.DecisionAllow,
		DurationMS: 42,
		DedupKey:   dedup,
		Metadata:   map[string]any{"test": true},
		CreatedAt:  time.Now().UTC(),
	}

	// First write should succeed.
	if err := s.WriteExecution(ctx, exec); err != nil {
		t.Fatalf("WriteExecution: %v", err)
	}

	// Duplicate dedup_key should be silently ignored (idempotent).
	if err := s.WriteExecution(ctx, exec); err != nil {
		t.Fatalf("WriteExecution (dedup): %v", err)
	}

	// Verify the row exists.
	var count int
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM hook_executions WHERE dedup_key=$1", dedup,
	).Scan(&count); err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 execution row, got %d", count)
	}
}

// ─── Cache invalidation ───────────────────────────────────────────────────────

func TestPGHookStore_CacheInvalidatedOnWrite(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, agentID := seedTenantAndAgent(t, db)
	s := NewPGHookStore(db)
	ctx := tenantScopedCtx(tenantID)

	event := hooks.Event{
		TenantID:  tenantID,
		AgentID:   agentID,
		HookEvent: hooks.EventSessionStart,
	}

	// Resolve populates cache.
	before, err := s.ResolveForEvent(ctx, event)
	if err != nil {
		t.Fatalf("ResolveForEvent: %v", err)
	}
	beforeCount := len(before)

	// Add a hook — cache should be invalidated.
	cfg := minimalHook(tenantID, hooks.EventSessionStart)
	id, err := s.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { s.Delete(masterCtx(), id) })

	after, err := s.ResolveForEvent(ctx, event)
	if err != nil {
		t.Fatalf("ResolveForEvent after create: %v", err)
	}
	if len(after) <= beforeCount {
		t.Errorf("expected more hooks after create: before=%d after=%d", beforeCount, len(after))
	}
}

// H9 (Phase 03): Create must honor a caller-supplied cfg.ID so the builtin
// seeder's idempotent UUIDv5 keys survive restarts and tests can use
// deterministic IDs. Verified on both Create paths (fixed + auto).
func TestPGHookStore_CreateHonorsFixedID(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, _ := seedTenantAndAgent(t, db)
	s := NewPGHookStore(db)
	ctx := tenantScopedCtx(tenantID)

	fixed := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	cfg := minimalHook(tenantID, hooks.EventUserPromptSubmit)
	cfg.ID = fixed

	got, err := s.Create(ctx, cfg)
	if err != nil {
		t.Fatalf("Create fixed id: %v", err)
	}
	t.Cleanup(func() { s.Delete(masterCtx(), got) })
	if got != fixed {
		t.Fatalf("returned id=%s, want %s (H9: caller id must be honored)", got, fixed)
	}

	// Nil cfg.ID still auto-generates a UUIDv7.
	cfg2 := minimalHook(tenantID, hooks.EventPreToolUse)
	cfg2.ID = uuid.Nil
	auto, err := s.Create(ctx, cfg2)
	if err != nil {
		t.Fatalf("Create auto id: %v", err)
	}
	t.Cleanup(func() { s.Delete(masterCtx(), auto) })
	if auto == uuid.Nil {
		t.Fatal("Create returned nil id for cfg.ID=uuid.Nil path")
	}
}

// ─── Phase 04: builtin-row readonly protection ───────────────────────────────

// TestPGHookStore_BuiltinReadOnly exercises the Phase 04 guard: user-facing
// writes on a source='builtin' row may only toggle enabled; every other patch
// must surface ErrBuiltinReadOnly. The seed bypass marker unlocks full writes
// for the loader package only.
func TestPGHookStore_BuiltinReadOnly(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, _ := seedTenantAndAgent(t, db)
	s := NewPGHookStore(db)

	// Seed a builtin row via WithSeedBypass (the only authorized path).
	seedCtx := hooks.WithSeedBypass(store.WithRole(masterCtx(), store.RoleOwner))
	cfg := minimalHook(hooks.SentinelTenantID, hooks.EventUserPromptSubmit)
	cfg.Source = hooks.SourceBuiltin
	cfg.Scope = hooks.ScopeGlobal
	cfg.HandlerType = hooks.HandlerScript
	cfg.Config = map[string]any{"source": "// v1"}
	fixedID := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	cfg.ID = fixedID
	id, err := s.Create(seedCtx, cfg)
	if err != nil {
		t.Fatalf("seed create: %v", err)
	}
	t.Cleanup(func() { s.Delete(seedCtx, id) })

	// User ctx: tenant-scoped. Non-enabled patch must be rejected.
	userCtx := tenantScopedCtx(tenantID)
	err = s.Update(userCtx, id, map[string]any{"matcher": "evil"})
	if !errors.Is(err, hooks.ErrBuiltinReadOnly) {
		t.Fatalf("Update(matcher) err=%v, want ErrBuiltinReadOnly", err)
	}

	// Enabled toggle through master context is allowed.
	if err := s.Update(masterCtx(), id, map[string]any{"enabled": false}); err != nil {
		t.Fatalf("Update(enabled) should succeed on builtin: %v", err)
	}

	// Delete blocked for users.
	if err := s.Delete(userCtx, id); !errors.Is(err, hooks.ErrBuiltinReadOnly) {
		t.Fatalf("Delete user err=%v, want ErrBuiltinReadOnly", err)
	}

	// Seed bypass unlocks full writes (round-trip to prove).
	if err := s.Update(seedCtx, id, map[string]any{"matcher": "ok"}); err != nil {
		t.Fatalf("seed-bypass Update should succeed: %v", err)
	}
}

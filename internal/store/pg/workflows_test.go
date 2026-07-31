package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// These run against a real Postgres because the things most likely to be wrong
// are SQL-level: the case-insensitive unique index, the tenant predicates, and
// whether NOT NULL defaults actually keep graph/compiled non-nil on read. A mock
// would happily agree with a broken schema.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Skipf("no database: %v", err)
	}

	// Minimal fixtures: the real schema has a large dependency graph, and this
	// table only needs tenants to exist for the FK.
	ctx := context.Background()
	for _, stmt := range []string{
		`CREATE EXTENSION IF NOT EXISTS pgcrypto`,
		`CREATE TABLE IF NOT EXISTS tenants (id UUID PRIMARY KEY DEFAULT gen_random_uuid())`,
		`DROP TABLE IF EXISTS workflows`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("fixture %q: %v", stmt, err)
		}
	}
	// Apply the migration under test verbatim, so a mistake in the .sql file is
	// caught here rather than on deploy.
	sqlBytes, err := os.ReadFile("../../../migrations/000083_workflows.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.ExecContext(ctx, string(sqlBytes)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func newTenant(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO tenants DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	return id
}

func TestWorkflowCRUD(t *testing.T) {
	db := testDB(t)
	s := NewPGWorkflowStore(db)
	ctx := context.Background()
	tenant := newTenant(t, db)

	w := &store.Workflow{TenantID: tenant, Name: "Competitor digest"}
	if err := s.Create(ctx, w); err != nil {
		t.Fatalf("create: %v", err)
	}
	if w.ID == uuid.Nil || w.CreatedAt.IsZero() {
		t.Fatal("create did not populate id/created_at")
	}
	// New workflows must be DISARMED: creating one on the canvas should never
	// start firing something before the author has finished building it.
	if w.Enabled {
		t.Error("a new workflow defaulted to enabled")
	}

	got, err := s.Get(ctx, tenant, w.ID)
	if err != nil || got == nil {
		t.Fatalf("get: %v (nil=%v)", err, got == nil)
	}
	// NOT NULL + default '{}' should mean readers never see nil and can unmarshal
	// without a length check.
	if len(got.Graph) == 0 || len(got.Compiled) == 0 {
		t.Errorf("graph/compiled came back empty: %q / %q", got.Graph, got.Compiled)
	}

	got.Name = "Weekly digest"
	got.Enabled = true
	got.Graph = json.RawMessage(`{"nodes":[{"id":"t1","type":"trigger"}],"edges":[]}`)
	if err := s.Update(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	after, _ := s.Get(ctx, tenant, w.ID)
	if after.Name != "Weekly digest" || !after.Enabled {
		t.Errorf("update did not persist: %+v", after)
	}
	if !json.Valid(after.Graph) || string(after.Graph) == "{}" {
		t.Errorf("graph did not round-trip: %s", after.Graph)
	}

	if err := s.Delete(ctx, tenant, w.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if gone, _ := s.Get(ctx, tenant, w.ID); gone != nil {
		t.Error("workflow survived delete")
	}
}

// The name index is on lower(name): "Digest" and "digest" must collide, because
// the UI names workflows in prose and two of them would make every message
// ambiguous.
func TestWorkflowNameIsCaseInsensitivePerTenant(t *testing.T) {
	db := testDB(t)
	s := NewPGWorkflowStore(db)
	ctx := context.Background()
	a := newTenant(t, db)
	b := newTenant(t, db)

	if err := s.Create(ctx, &store.Workflow{TenantID: a, Name: "Digest"}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	err := s.Create(ctx, &store.Workflow{TenantID: a, Name: "digest"})
	if !errors.Is(err, store.ErrWorkflowNameTaken) {
		t.Errorf("case-variant name in the same tenant gave %v, want ErrWorkflowNameTaken", err)
	}
	// A different tenant may reuse the name — the index is per tenant.
	if err := s.Create(ctx, &store.Workflow{TenantID: b, Name: "Digest"}); err != nil {
		t.Errorf("another tenant could not reuse the name: %v", err)
	}
}

// Tenant scoping is the security property of this store: one tenant must never
// read, edit, arm or delete another's automation.
func TestWorkflowTenantIsolation(t *testing.T) {
	db := testDB(t)
	s := NewPGWorkflowStore(db)
	ctx := context.Background()
	owner := newTenant(t, db)
	other := newTenant(t, db)

	w := &store.Workflow{TenantID: owner, Name: "Owned"}
	if err := s.Create(ctx, w); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Read: indistinguishable from "does not exist", so a guessed id is not an
	// existence oracle.
	got, err := s.Get(ctx, other, w.ID)
	if err != nil || got != nil {
		t.Errorf("cross-tenant get returned %v (err %v)", got, err)
	}

	// Write, including ARMING it.
	if err := s.Update(ctx, &store.Workflow{ID: w.ID, TenantID: other, Name: "Hijacked", Enabled: true}); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("cross-tenant update returned %v, want ErrNoRows", err)
	}
	if err := s.Delete(ctx, other, w.ID); err != nil {
		t.Errorf("cross-tenant delete errored oddly: %v", err)
	}
	still, _ := s.Get(ctx, owner, w.ID)
	if still == nil {
		t.Fatal("cross-tenant delete removed the owner's workflow")
	}
	if still.Name != "Owned" || still.Enabled {
		t.Errorf("cross-tenant update leaked through: %+v", still)
	}
}

// A user's save must not clobber what the reconciler recorded, and vice versa.
func TestCompileResultIsSeparateFromAuthoredFields(t *testing.T) {
	db := testDB(t)
	s := NewPGWorkflowStore(db)
	ctx := context.Background()
	tenant := newTenant(t, db)

	w := &store.Workflow{TenantID: tenant, Name: "Compiled"}
	if err := s.Create(ctx, w); err != nil {
		t.Fatalf("create: %v", err)
	}

	boom := "trigger node has no schedule"
	if err := s.SetCompileResult(ctx, tenant, w.ID, json.RawMessage(`{"cron_ids":["c1"]}`), &boom); err != nil {
		t.Fatalf("set compile result: %v", err)
	}

	// An ordinary save must leave compile state alone.
	w.Name = "Renamed"
	if err := s.Update(ctx, w); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := s.Get(ctx, tenant, w.ID)
	if got.CompileError == nil || *got.CompileError != boom {
		t.Errorf("a user save wiped compile_error: %v", got.CompileError)
	}
	if string(got.Compiled) != `{"cron_ids": ["c1"]}` && string(got.Compiled) != `{"cron_ids":["c1"]}` {
		t.Errorf("a user save wiped compiled: %s", got.Compiled)
	}

	// Clearing the error on a successful compile.
	if err := s.SetCompileResult(ctx, tenant, w.ID, json.RawMessage(`{"cron_ids":["c2"]}`), nil); err != nil {
		t.Fatalf("clear compile error: %v", err)
	}
	if got, _ = s.Get(ctx, tenant, w.ID); got.CompileError != nil {
		t.Errorf("compile_error not cleared: %v", *got.CompileError)
	}
}

// The reconciler rebuilds schedules from this, so it must return armed workflows
// across every tenant and nothing that is disarmed.
func TestListEnabledCrossesTenantsAndExcludesDisarmed(t *testing.T) {
	db := testDB(t)
	s := NewPGWorkflowStore(db)
	ctx := context.Background()
	a := newTenant(t, db)
	b := newTenant(t, db)

	if err := s.Create(ctx, &store.Workflow{TenantID: a, Name: "armed-a", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, &store.Workflow{TenantID: b, Name: "armed-b", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, &store.Workflow{TenantID: a, Name: "draft", Enabled: false}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListEnabled(ctx)
	if err != nil {
		t.Fatalf("list enabled: %v", err)
	}
	names := map[string]bool{}
	for _, w := range got {
		names[w.Name] = true
	}
	if !names["armed-a"] || !names["armed-b"] {
		t.Errorf("missing armed workflows: %v", names)
	}
	if names["draft"] {
		t.Error("a disarmed workflow would have been scheduled")
	}
}

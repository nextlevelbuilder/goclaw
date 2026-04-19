//go:build integration

package integration

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/edition"
	"github.com/nextlevelbuilder/goclaw/internal/hooks"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ─── Phase 08 shared test helpers ────────────────────────────────────────────
//
// Helpers scoped to the Phase 08 hook integration tests. Do NOT extend with
// generic store / channel helpers — keep additions hook-specific so the file
// stays focused.

// withEdition flips edition.Current() for the duration of the test, restoring
// the prior value on cleanup. Used by tests that exercise the edition-gated
// migration + validation paths (E bucket, F5).
func withEdition(t *testing.T, e edition.Edition) {
	t.Helper()
	prev := edition.Current()
	edition.SetCurrent(e)
	t.Cleanup(func() { edition.SetCurrent(prev) })
}

// seedExtraTenant inserts a tenant row with auto-cleanup. Returns the new id.
// Used by tests that need a SECOND tenant alongside seedTenantAgent's pair so
// we can assert isolation between two real tenants without re-running the
// full agent fixture chain.
func seedExtraTenant(t *testing.T, db *sql.DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES ($1, $2, $3, 'active')
		 ON CONFLICT DO NOTHING`,
		id, "test-tenant-"+id.String()[:8], "t"+id.String()[:8])
	if err != nil {
		t.Fatalf("seed extra tenant: %v", err)
	}
	t.Cleanup(func() { db.Exec("DELETE FROM tenants WHERE id = $1", id) })
	return id
}

// purgeAgentHooksByTenant cleans hook + execution rows for one tenant. Used by
// the seed-reconciliation tests that re-run Seed() and need a clean slate.
func purgeAgentHooksByTenant(t *testing.T, db *sql.DB, tenantID uuid.UUID) {
	t.Helper()
	db.Exec(`DELETE FROM hook_executions WHERE hook_id IN (SELECT id FROM hooks WHERE tenant_id = $1)`, tenantID)
	db.Exec(`DELETE FROM hooks WHERE tenant_id = $1`, tenantID)
}

// purgeAgentHookByID removes one hook row + its audit. Standard cleanup
// pattern for tests that create a single hook explicitly.
func purgeAgentHookByID(t *testing.T, db *sql.DB, id uuid.UUID) {
	t.Helper()
	db.Exec(`DELETE FROM hook_executions WHERE hook_id = $1`, id)
	db.Exec(`DELETE FROM hooks WHERE id = $1`, id)
}

// pollAuditDecision polls hook_executions for one row matching (hookID,
// decision) — returns true once observed or false on timeout. Stricter
// equivalent of pollAuditCount(want=1) when callers want a boolean check.
func pollAuditDecision(t *testing.T, db *sql.DB, hookID uuid.UUID, decision string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var n int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM hook_executions WHERE hook_id = $1 AND decision = $2`,
			hookID, decision,
		).Scan(&n); err == nil && n > 0 {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// crossTenantOwnerCtx returns a context bypassing tenant scope AND carrying
// the owner role — required for tests that reach into rows across tenants
// (e.g. seed reconciliation, command-autodisable migration which lists
// every tenant's enabled rows).
func crossTenantOwnerCtx() context.Context {
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	ctx = store.WithCrossTenant(ctx)
	ctx = store.WithRole(ctx, store.RoleOwner)
	return ctx
}

// hookCfgScript returns a baseline script hook config. Tests override fields
// (scope, source, event, etc.) before passing to Create. Centralizes the
// boilerplate so each test's intent stays one-line-clear.
func hookCfgScript(tenantID uuid.UUID, ev hooks.HookEvent, source string, src string) hooks.HookConfig {
	return hooks.HookConfig{
		TenantID:    tenantID,
		Scope:       hooks.ScopeTenant,
		Event:       ev,
		HandlerType: hooks.HandlerScript,
		Config:      map[string]any{"source": src},
		TimeoutMS:   2000,
		OnTimeout:   hooks.DecisionAllow,
		Priority:    100,
		Enabled:     true,
		Version:     1,
		Source:      source,
		Metadata:    map[string]any{},
	}
}

//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"
)

// TestSQLiteAgentStoreList_NilTenant_FailsClosed verifies List returns an
// empty result (not an unscoped cross-tenant leak) when the incoming context
// carries no tenant scope — matching the fail-closed contract already
// enforced by GetByID/GetByKey. Regression test for the bug where List
// silently omitted the tenant_id filter whenever the context tenant was nil.
func TestSQLiteAgentStoreList_NilTenant_FailsClosed(t *testing.T) {
	db := newHookTestDB(t)
	tenantID, agentID := seedHookTenantAgent(t, db)
	// seedHookTenantAgent leaves display_name NULL, which scanAgentRow can't
	// scan into a plain string — set it so List's underlying scan succeeds.
	if _, err := db.Exec(`UPDATE agents SET display_name = 'test agent' WHERE id = ?`, agentID.String()); err != nil {
		t.Fatalf("set display_name: %v", err)
	}
	agentStore := NewSQLiteAgentStore(db)

	// Sanity check: the seeded agent IS visible when properly tenant-scoped.
	scoped, err := agentStore.List(sqliteTenantCtx(tenantID), "")
	if err != nil {
		t.Fatalf("List (tenant-scoped): %v", err)
	}
	if len(scoped) == 0 {
		t.Fatal("expected at least one agent when tenant-scoped, got none")
	}

	// No tenant in context at all (uuid.Nil, not master, not cross-tenant):
	// must fail closed (empty, no error), never return unscoped rows.
	unscoped, err := agentStore.List(context.Background(), "")
	if err != nil {
		t.Fatalf("List (no tenant context): unexpected error %v", err)
	}
	if len(unscoped) != 0 {
		t.Fatalf("List (no tenant context) leaked %d rows across tenants, want 0 (fail-closed)", len(unscoped))
	}
}

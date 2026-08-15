//go:build integration

package integration

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/backup"
)

// TestTenantRestore_DeleteTenantData_PreservesTenantsRow verifies that
// replace mode's child-cleanup path does NOT delete the tenants row itself.
//
// Prior to PR #920 follow-up fix, deleteTenantData generated
// `DELETE FROM tenants WHERE id = $1` as the final iteration. This fails
// under PG FK restrict for any tenant that has rows in diagnostic tables
// (traces, activity_logs, usage_snapshots, spans, embedding_cache, etc.)
// because those tables reference tenants(id) without ON DELETE CASCADE.
//
// This regression guard seeds activity_logs (a table excluded from the
// backup registry) and then invokes the replace-mode cleanup path. It
// must complete without FK violations AND leave the tenants row intact.
func TestTenantRestore_DeleteTenantData_PreservesTenantsRow(t *testing.T) {
	db := testDB(t)
	tenantID, agent1ID := seedTenantAgent(t, db)

	// Capture tenant metadata.
	var tenantSlugBefore, tenantNameBefore string
	if err := db.QueryRow(`SELECT slug, name FROM tenants WHERE id = $1`, tenantID).
		Scan(&tenantSlugBefore, &tenantNameBefore); err != nil {
		t.Fatalf("fetch tenant: %v", err)
	}

	// Seed diagnostic rows that are NOT in TenantTables() (excluded intentionally).
	// These would cause FK violation on DELETE FROM tenants if the fix is missing.
	seedActivityLog(t, db, tenantID, "pre-delete-1")
	seedActivityLog(t, db, tenantID, "pre-delete-2")
	t.Cleanup(func() {
		db.Exec(`DELETE FROM activity_logs WHERE tenant_id = $1`, tenantID)
	})

	// Add a second agent — this is the target of the cleanup.
	agent2ID := uuid.New()
	_, err := db.Exec(`
		INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		VALUES ($1, $2, $3, 'predefined', 'active', 'test', 'test-model', 'test-owner')`,
		agent2ID, tenantID, "extra-"+agent2ID.String()[:8])
	if err != nil {
		t.Fatalf("insert agent2: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM agents WHERE id = $1`, agent2ID)
	})

	// Invoke the replace-mode cleanup path directly via the exported helper.
	// Before the fix: this errors with "update or delete on table \"tenants\"
	// violates foreign key constraint ... on table \"activity_logs\"".
	// After the fix: completes cleanly, agents wiped, tenants row preserved.
	tables := backup.TenantTables()
	if err := backup.DeleteTenantDataForTest(context.Background(), db, tenantID, tables); err != nil {
		// Surface FK violation clearly if the fix regresses.
		if strings.Contains(err.Error(), "foreign key") || strings.Contains(err.Error(), "tenants") {
			t.Fatalf("replace cleanup hit FK violation (fix regressed): %v", err)
		}
		t.Fatalf("deleteTenantData: %v", err)
	}

	// Tenants row preserved with original metadata.
	var slugAfter, nameAfter string
	if err := db.QueryRow(`SELECT slug, name FROM tenants WHERE id = $1`, tenantID).
		Scan(&slugAfter, &nameAfter); err != nil {
		if err == sql.ErrNoRows {
			t.Fatal("tenants row was deleted — fix regressed")
		}
		t.Fatalf("fetch tenant post-cleanup: %v", err)
	}
	if slugAfter != tenantSlugBefore || nameAfter != tenantNameBefore {
		t.Errorf("tenants metadata changed: slug=%q name=%q, want slug=%q name=%q",
			slugAfter, nameAfter, tenantSlugBefore, tenantNameBefore)
	}

	// All child agents wiped.
	var agentCount int
	db.QueryRow(`SELECT COUNT(*) FROM agents WHERE tenant_id = $1`, tenantID).Scan(&agentCount)
	if agentCount != 0 {
		t.Errorf("agents after cleanup = %d, want 0 (all children wiped)", agentCount)
	}
	// agent1 also gone — replace cleanup is exhaustive for child tables.
	var hasAgent1 bool
	db.QueryRow(`SELECT EXISTS(SELECT 1 FROM agents WHERE id = $1)`, agent1ID).Scan(&hasAgent1)
	if hasAgent1 {
		t.Error("agent1 should be wiped by replace cleanup")
	}

	// Diagnostic rows untouched (excluded from backup registry).
	var logCount int
	db.QueryRow(`SELECT COUNT(*) FROM activity_logs WHERE tenant_id = $1`, tenantID).Scan(&logCount)
	if logCount != 2 {
		t.Errorf("activity_logs = %d, want 2 (excluded from cleanup)", logCount)
	}
}

// seedActivityLog inserts a minimal activity_logs row scoped to tenantID.
// activity_logs is intentionally excluded from TenantTables(), so it exercises
// the FK-to-tenants-without-CASCADE path.
func seedActivityLog(t *testing.T, db *sql.DB, tenantID uuid.UUID, note string) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO activity_logs (id, tenant_id, actor_type, actor_id, action, entity_type, entity_id)
		VALUES ($1, $2, 'user', 'test-user', 'test', 'test', $3)`,
		uuid.New(), tenantID, note)
	if err != nil {
		t.Fatalf("seed activity_log: %v", err)
	}
}

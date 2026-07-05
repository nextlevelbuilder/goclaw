//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestSQLiteDeleteTeam_PreservesTeamVaultDocs is a regression test for issue #1077.
//
// Deleting a team must not fail when the team owns vault documents. Team-scoped
// docs (scope='team', agent_id IS NULL) must be preserved by converting them to
// scope='shared' with team_id NULL. Without the fix, the FK ON DELETE SET NULL
// leaves scope='team' with a NULL team_id, which violates the
// vault_documents_scope_consistency CHECK and aborts the whole delete.
func TestSQLiteDeleteTeam_PreservesTeamVaultDocs(t *testing.T) {
	db := newTeamVaultScopeDB(t)
	tenantID := store.GenNewID()
	mustExec(t, db, `INSERT INTO tenants (id, name, slug, status, settings, created_at, updated_at)
		VALUES (?, 'test-tenant', 'test-tenant', 'active', '{}', datetime('now'), datetime('now'))`, tenantID)
	agentID := store.GenNewID()
	mustExec(t, db, `INSERT INTO agents (id, agent_key, owner_id, model, tenant_id)
		VALUES (?, 'lead', 'owner', 'gpt', ?)`, agentID, tenantID)

	teamID := store.GenNewID()
	mustExec(t, db, `INSERT INTO agent_teams (id, name, lead_agent_id, created_by, tenant_id)
		VALUES (?, 'team-a', ?, 'owner', ?)`, teamID, agentID, tenantID)

	// Team-scoped doc (agent_id NULL) — the exact shape that triggers the bug.
	teamDocID := store.GenNewID()
	mustExec(t, db, `INSERT INTO vault_documents (id, tenant_id, team_id, scope, path)
		VALUES (?, ?, ?, 'team', 'teams/a/note.md')`, teamDocID, tenantID, teamID)

	// Custom-scope doc carrying team_id + agent_id — must NOT be clobbered.
	customDocID := store.GenNewID()
	mustExec(t, db, `INSERT INTO vault_documents (id, tenant_id, agent_id, team_id, scope, path)
		VALUES (?, ?, ?, ?, 'custom', 'teams/a/custom.md')`, customDocID, tenantID, agentID, teamID)

	teamStore := NewSQLiteTeamStore(db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	if err := teamStore.DeleteTeam(ctx, teamID); err != nil {
		t.Fatalf("DeleteTeam failed: %v", err)
	}

	// Team was actually deleted.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM agent_teams WHERE id = ?`, teamID).Scan(&count); err != nil {
		t.Fatalf("count teams: %v", err)
	}
	if count != 0 {
		t.Fatalf("team not deleted, count=%d", count)
	}

	// Team doc preserved as 'shared'.
	assertVaultScope(t, db, teamDocID, "shared", false)
	// Custom doc keeps its scope; team_id cleared by FK SET NULL.
	assertVaultScope(t, db, customDocID, "custom", false)
}

func assertVaultScope(t *testing.T, db *sql.DB, docID uuid.UUID, wantScope string, wantTeam bool) {
	t.Helper()
	var scope string
	var teamID sql.NullString
	if err := db.QueryRow(`SELECT scope, team_id FROM vault_documents WHERE id = ?`, docID).Scan(&scope, &teamID); err != nil {
		t.Fatalf("query doc %s: %v", docID, err)
	}
	if scope != wantScope {
		t.Errorf("doc %s scope = %q, want %q", docID, scope, wantScope)
	}
	if teamID.Valid != wantTeam {
		t.Errorf("doc %s team_id present = %v, want %v (value=%q)", docID, teamID.Valid, wantTeam, teamID.String)
	}
}

func newTeamVaultScopeDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "team_vault_scope.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	return db
}

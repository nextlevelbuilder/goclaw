//go:build integration

package integration

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
)

// TestStoreVault_ScopeCheck verifies the vault_documents_scope_consistency CHECK constraint
// (added by migration 000055 NOT VALID) rejects invalid scope/ownership combinations on
// new inserts while accepting valid ones.
//
// NOT VALID means existing rows are not scanned but new writes are still gated — the CHECK
// fires on INSERT/UPDATE regardless of VALID status.
func TestStoreVault_ScopeCheck(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	tid := tenantID.String()
	aid := agentID.String()

	// unique path suffix per case to avoid conflicts with other test runs.
	suffix := uuid.New().String()[:8]

	cases := []struct {
		name    string
		query   string
		args    []any
		wantErr bool
	}{
		{
			name: "reject_personal_with_null_agent_id",
			// scope='personal' requires agent_id NOT NULL
			query: `INSERT INTO vault_documents
				(id, tenant_id, scope, path, title, doc_type, content_hash)
				VALUES ($1, $2, 'personal', $3, 'bad', 'note', 'h1')`,
			args:    []any{uuid.New().String(), tid, "scope-check/bad-personal-" + suffix + ".md"},
			wantErr: true,
		},
		{
			name: "reject_team_with_null_team_id",
			// scope='team' requires team_id NOT NULL
			query: `INSERT INTO vault_documents
				(id, tenant_id, scope, path, title, doc_type, content_hash)
				VALUES ($1, $2, 'team', $3, 'bad', 'note', 'h2')`,
			args:    []any{uuid.New().String(), tid, "scope-check/bad-team-" + suffix + ".md"},
			wantErr: true,
		},
		{
			name: "reject_shared_with_non_null_agent_id",
			// scope='shared' requires agent_id IS NULL
			query: `INSERT INTO vault_documents
				(id, tenant_id, agent_id, scope, path, title, doc_type, content_hash)
				VALUES ($1, $2, $3, 'shared', $4, 'bad', 'note', 'h3')`,
			args:    []any{uuid.New().String(), tid, aid, "scope-check/bad-shared-" + suffix + ".md"},
			wantErr: true,
		},
		{
			name: "accept_custom_scope_with_null_agent_id",
			// scope='custom' has no constraint — should succeed
			query: `INSERT INTO vault_documents
				(id, tenant_id, scope, path, title, doc_type, content_hash)
				VALUES ($1, $2, 'custom', $3, 'ok', 'note', 'h4')`,
			args:    []any{uuid.New().String(), tid, "scope-check/ok-custom-" + suffix + ".md"},
			wantErr: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := db.Exec(tc.query, tc.args...)
			if tc.wantErr && err == nil {
				t.Errorf("expected constraint violation error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
		})
	}
}

// TestStoreVault_TeamDeletePreservesDocs is a regression test for issue #1077.
//
// Deleting a team that owns vault documents must succeed. Team-scoped docs
// (scope='team', agent_id IS NULL) are preserved as scope='shared' via the
// vault_docs_team_null_scope_fix() trigger. Before the fix the trigger forced
// scope='personal' unconditionally, which — combined with agent_id IS NULL —
// violated vault_documents_scope_consistency and aborted the delete (SQLSTATE 23514).
func TestStoreVault_TeamDeletePreservesDocs(t *testing.T) {
	db := testDB(t)
	tenantID, agentID := seedTenantAgent(t, db)
	teamID, _ := seedTeam(t, db, tenantID, agentID)

	suffix := uuid.New().String()[:8]
	teamDocID := uuid.New().String()
	customDocID := uuid.New().String()
	t.Cleanup(func() {
		db.Exec(`DELETE FROM vault_documents WHERE id IN ($1, $2)`, teamDocID, customDocID)
	})

	// Team-scoped doc (agent_id NULL) — the exact shape that triggers the bug.
	if _, err := db.Exec(`INSERT INTO vault_documents
		(id, tenant_id, team_id, scope, path, title, doc_type, content_hash)
		VALUES ($1, $2, $3, 'team', $4, 'team-doc', 'note', 'h1')`,
		teamDocID, tenantID, teamID, "team-del/"+suffix+"-team.md"); err != nil {
		t.Fatalf("insert team doc: %v", err)
	}
	// Custom-scope doc carrying team_id + agent_id — must NOT be clobbered.
	if _, err := db.Exec(`INSERT INTO vault_documents
		(id, tenant_id, agent_id, team_id, scope, path, title, doc_type, content_hash)
		VALUES ($1, $2, $3, $4, 'custom', $5, 'custom-doc', 'note', 'h2')`,
		customDocID, tenantID, agentID, teamID, "team-del/"+suffix+"-custom.md"); err != nil {
		t.Fatalf("insert custom doc: %v", err)
	}

	// Delete the team: exercises FK ON DELETE SET NULL → scope-fix trigger → CHECK.
	if _, err := db.Exec(`DELETE FROM agent_teams WHERE id = $1`, teamID); err != nil {
		t.Fatalf("delete team failed (issue #1077 regression): %v", err)
	}

	// Team doc preserved as 'shared', team_id cleared.
	assertPGVaultScope(t, db, teamDocID, "shared")
	// Custom doc keeps its scope, team_id cleared by FK SET NULL.
	assertPGVaultScope(t, db, customDocID, "custom")
}

func assertPGVaultScope(t *testing.T, db *sql.DB, docID, wantScope string) {
	t.Helper()
	var scope string
	var teamID sql.NullString
	if err := db.QueryRow(`SELECT scope, team_id FROM vault_documents WHERE id = $1`, docID).Scan(&scope, &teamID); err != nil {
		t.Fatalf("query doc %s: %v", docID, err)
	}
	if scope != wantScope {
		t.Errorf("doc %s scope = %q, want %q", docID, scope, wantScope)
	}
	if teamID.Valid {
		t.Errorf("doc %s team_id should be NULL after team delete, got %q", docID, teamID.String)
	}
}

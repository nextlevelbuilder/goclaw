//go:build integration

package integration

import (
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

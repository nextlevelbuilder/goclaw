//go:build integration

package integration

import (
	"database/sql"
	"testing"

	"github.com/google/uuid"
)

// TestStoreVault_OrphanCollision is the regression for #1550.
//
// vault_documents.agent_id and team_id are FKs with ON DELETE SET NULL, and the scope
// triggers move a row whose owner is gone to scope='shared'. The scope-consistency CHECK
// requires scope='shared' to carry both ids NULL, and before migration 000099 the unique
// index COALESCEd both to one sentinel, so every orphan in a tenant shared the key
// (tenant_id, sentinel, sentinel, 'shared', path). Deleting a second owner that had a
// document at a path some earlier deleted owner already orphaned aborted the whole
// delete with SQLSTATE 23505, and the agent could then not be deleted by any route.
//
// Orphans reach 'shared' from both the agent trigger (000046) and the team trigger
// (000089), so all three pairings are covered here. Migration 000099 records the lost
// owner in orphaned_from_id and includes it in the index; the documents are preserved
// at their original path.
func TestStoreVault_OrphanCollision(t *testing.T) {
	db := testDB(t)
	tenantID, _ := seedTenantAgent(t, db)
	suffix := uuid.New().String()[:8]

	t.Run("two_agents_same_path", func(t *testing.T) {
		path := "orphan-collision/agents-" + suffix + ".md"
		first := seedAgentIn(t, db, tenantID)
		second := seedAgentIn(t, db, tenantID)
		insertAgentDoc(t, db, tenantID, first, path)
		insertAgentDoc(t, db, tenantID, second, path)

		deleteAgent(t, db, first)  // orphans the first document
		deleteAgent(t, db, second) // aborted with 23505 before 000099

		assertOrphans(t, db, tenantID, path, 2)
	})

	t.Run("two_teams_same_path", func(t *testing.T) {
		// Not covered by the #1077 fix: the team trigger also lands on scope='shared'.
		path := "orphan-collision/teams-" + suffix + ".md"
		lead := seedAgentIn(t, db, tenantID)
		first := seedTeamIn(t, db, tenantID, lead)
		second := seedTeamIn(t, db, tenantID, lead)
		insertTeamDoc(t, db, tenantID, first, path)
		insertTeamDoc(t, db, tenantID, second, path)

		deleteTeam(t, db, first)
		deleteTeam(t, db, second)

		assertOrphans(t, db, tenantID, path, 2)
	})

	t.Run("agent_and_team_same_path", func(t *testing.T) {
		path := "orphan-collision/mixed-" + suffix + ".md"
		agentID := seedAgentIn(t, db, tenantID)
		lead := seedAgentIn(t, db, tenantID)
		teamID := seedTeamIn(t, db, tenantID, lead)
		insertAgentDoc(t, db, tenantID, agentID, path)
		insertTeamDoc(t, db, tenantID, teamID, path)

		deleteAgent(t, db, agentID)
		deleteTeam(t, db, teamID)

		assertOrphans(t, db, tenantID, path, 2)
	})

	t.Run("live_uniqueness_is_unchanged", func(t *testing.T) {
		// The index still rejects two live documents of one owner at one path.
		path := "orphan-collision/live-" + suffix + ".md"
		agentID := seedAgentIn(t, db, tenantID)
		insertAgentDoc(t, db, tenantID, agentID, path)
		_, err := db.Exec(
			`INSERT INTO vault_documents (id, tenant_id, agent_id, scope, path, title, doc_type, content_hash)
			 VALUES ($1, $2, $3, 'personal', $4, 'dup', 'note', 'h')`,
			uuid.New(), tenantID, agentID, path)
		if err == nil {
			t.Fatal("a second live document at the same path for one agent was accepted")
		}
	})
}

// assertOrphans checks that want documents sit at path as orphans, each carrying a
// distinct provenance, and that none of them was moved or dropped.
func assertOrphans(t *testing.T, db *sql.DB, tenantID uuid.UUID, path string, want int) {
	t.Helper()
	var total, withProvenance, distinct int
	err := db.QueryRow(
		`SELECT count(*), count(orphaned_from_id), count(DISTINCT orphaned_from_id)
		   FROM vault_documents
		  WHERE tenant_id = $1 AND path = $2 AND scope = 'shared'
		    AND agent_id IS NULL AND team_id IS NULL`,
		tenantID, path).Scan(&total, &withProvenance, &distinct)
	if err != nil {
		t.Fatalf("count orphans: %v", err)
	}
	if total != want {
		t.Fatalf("orphans at %s = %d, want %d", path, total, want)
	}
	if withProvenance != want {
		t.Fatalf("orphans carrying provenance = %d, want %d", withProvenance, want)
	}
	if distinct != want {
		t.Fatalf("distinct provenance values = %d, want %d", distinct, want)
	}
}

func seedAgentIn(t *testing.T, db *sql.DB, tenantID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES ($1, $2, $3, 'predefined', 'active', 'test', 'test-model', 'test-owner')`,
		id, tenantID, "orphan-"+id.String()[:8])
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM agents WHERE id = $1`, id) })
	return id
}

func seedTeamIn(t *testing.T, db *sql.DB, tenantID, leadAgentID uuid.UUID) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(
		`INSERT INTO agent_teams (id, tenant_id, name, lead_agent_id, created_by)
		 VALUES ($1, $2, $3, $4, 'test')`,
		id, tenantID, "orphan-team-"+id.String()[:8], leadAgentID)
	if err != nil {
		t.Fatalf("seed team: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM agent_teams WHERE id = $1`, id) })
	return id
}

func insertAgentDoc(t *testing.T, db *sql.DB, tenantID, agentID uuid.UUID, path string) {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(
		`INSERT INTO vault_documents (id, tenant_id, agent_id, scope, path, title, doc_type, content_hash)
		 VALUES ($1, $2, $3, 'personal', $4, 'doc', 'note', 'h')`,
		id, tenantID, agentID, path)
	if err != nil {
		t.Fatalf("insert agent doc: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM vault_documents WHERE id = $1`, id) })
}

func insertTeamDoc(t *testing.T, db *sql.DB, tenantID, teamID uuid.UUID, path string) {
	t.Helper()
	id := uuid.New()
	_, err := db.Exec(
		`INSERT INTO vault_documents (id, tenant_id, team_id, scope, path, title, doc_type, content_hash)
		 VALUES ($1, $2, $3, 'team', $4, 'doc', 'note', 'h')`,
		id, tenantID, teamID, path)
	if err != nil {
		t.Fatalf("insert team doc: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM vault_documents WHERE id = $1`, id) })
}

func deleteAgent(t *testing.T, db *sql.DB, id uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM agents WHERE id = $1`, id); err != nil {
		t.Fatalf("delete agent %s: %v", id, err)
	}
}

func deleteTeam(t *testing.T, db *sql.DB, id uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM agent_teams WHERE id = $1`, id); err != nil {
		t.Fatalf("delete team %s: %v", id, err)
	}
}

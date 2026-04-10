//go:build sqlite || sqliteonly

package sqlitestore

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// TestEnsureSchema_FreshDB verifies schema.sql + all migrations apply cleanly on a fresh DB.
func TestEnsureSchema_FreshDB(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema (fresh) failed: %v", err)
	}

	// Verify schema version matches current
	var version int
	if err := db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version); err != nil {
		t.Fatalf("read schema_version: %v", err)
	}
	if version != SchemaVersion {
		t.Errorf("schema version = %d, want %d", version, SchemaVersion)
	}

	// Verify vault_documents table has expected columns (team_id, custom_scope, summary)
	rows, err := db.Query("PRAGMA table_info(vault_documents)")
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()

	cols := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notnull int
		var dflt *string
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		cols[name] = true
	}
	for _, want := range []string{"team_id", "custom_scope", "summary"} {
		if !cols[want] {
			t.Errorf("vault_documents missing column %q", want)
		}
	}
}

// TestEnsureSchema_MigrationV11Only verifies migrations from v11 onward
// apply correctly on a DB built at version 11.
func TestEnsureSchema_MigrationV11Only(t *testing.T) {
	db := openTestDBAtVersion(t, 11)

	// Re-apply — should run migrations 11→SchemaVersion
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema (v11→current) failed: %v", err)
	}

	var version int
	db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version)
	if version != SchemaVersion {
		t.Errorf("schema version = %d, want %d", version, SchemaVersion)
	}
}

// TestEnsureSchema_IdempotentRerun verifies EnsureSchema can be called twice without error.
func TestEnsureSchema_IdempotentRerun(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("first EnsureSchema: %v", err)
	}
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("second EnsureSchema (idempotent) failed: %v", err)
	}
}

// TestEnsureSchema_MigrationV11_SeedsAgentFiles verifies migration 11→12 seeds
// AGENTS_CORE.md and AGENTS_TASK.md and removes AGENTS_MINIMAL.md.
func TestEnsureSchema_MigrationV11_SeedsAgentFiles(t *testing.T) {
	db := openTestDBAtVersion(t, 11)

	// Use master tenant (seeded by seedMasterTenant)
	tenantID := "0193a5b0-7000-7000-8000-000000000001"
	agentID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	_, err := db.Exec(`INSERT INTO agents (id, tenant_id, agent_key, display_name, provider, model, agent_type, owner_id)
		VALUES (?, ?, 'test-agent', 'Test', 'test', 'test', 'predefined', 'owner-1')`,
		agentID, tenantID)
	if err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	// Insert an AGENTS_MINIMAL.md that should be cleaned up
	db.Exec(`INSERT INTO agent_context_files (id, agent_id, file_name, content, tenant_id, created_at, updated_at)
		VALUES ('min-id', ?, 'AGENTS_MINIMAL.md', 'old minimal', ?, datetime('now'), datetime('now'))`,
		agentID, tenantID)

	// Re-apply — runs migrations 11→SchemaVersion (includes v11→12 seed)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema (re-apply from v11): %v", err)
	}

	// Verify AGENTS_CORE.md seeded
	var coreCount int
	db.QueryRow("SELECT COUNT(*) FROM agent_context_files WHERE agent_id = ? AND file_name = 'AGENTS_CORE.md'", agentID).Scan(&coreCount)
	if coreCount != 1 {
		t.Errorf("AGENTS_CORE.md count = %d, want 1", coreCount)
	}

	// Verify AGENTS_TASK.md seeded
	var taskCount int
	db.QueryRow("SELECT COUNT(*) FROM agent_context_files WHERE agent_id = ? AND file_name = 'AGENTS_TASK.md'", agentID).Scan(&taskCount)
	if taskCount != 1 {
		t.Errorf("AGENTS_TASK.md count = %d, want 1", taskCount)
	}

	// Verify AGENTS_MINIMAL.md removed
	var minCount int
	db.QueryRow("SELECT COUNT(*) FROM agent_context_files WHERE file_name = 'AGENTS_MINIMAL.md'").Scan(&minCount)
	if minCount != 0 {
		t.Errorf("AGENTS_MINIMAL.md count = %d, want 0 (should be deleted)", minCount)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := OpenDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// openTestDBAtVersion creates a fresh DB, applies full schema, then
// drops columns added by migrations > targetVersion so re-running
// EnsureSchema from that version exercises the real migration path.
//
// We accomplish this by applying schema at targetVersion: apply full
// schema.sql then set version = targetVersion. Migrations will ALTER
// TABLE ADD COLUMN — which only fails if the column already exists.
// To avoid that, we drop the columns that post-targetVersion migrations add.
func openTestDBAtVersion(t *testing.T, targetVersion int) *sql.DB {
	t.Helper()
	db := openTestDB(t)

	// Apply full schema first (creates all tables with all columns).
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Undo columns added by migrations after targetVersion.
	// SQLite DROP COLUMN support varies, so recreate affected tables.
	if targetVersion <= 11 {
		// Migration 12 adds recall_count, recall_score, last_recalled_at.
		// Recreate episodic_summaries without those columns.
		db.Exec(`CREATE TABLE episodic_summaries_old AS SELECT
			id, tenant_id, agent_id, user_id, session_key, summary, l0_abstract,
			key_topics, source_type, source_id, turn_count, token_count,
			created_at, expires_at, promoted_at
			FROM episodic_summaries`)
		db.Exec(`DROP TABLE episodic_summaries`)
		db.Exec(`CREATE TABLE episodic_summaries (
			id TEXT NOT NULL PRIMARY KEY, tenant_id TEXT NOT NULL, agent_id TEXT NOT NULL,
			user_id VARCHAR(255) NOT NULL DEFAULT '', session_key TEXT NOT NULL,
			summary TEXT NOT NULL, l0_abstract TEXT NOT NULL DEFAULT '',
			key_topics TEXT NOT NULL DEFAULT '[]', source_type TEXT NOT NULL DEFAULT 'session',
			source_id TEXT, turn_count INTEGER NOT NULL DEFAULT 0,
			token_count INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			expires_at TEXT, promoted_at TEXT)`)
		db.Exec(`INSERT INTO episodic_summaries SELECT * FROM episodic_summaries_old`)
		db.Exec(`DROP TABLE episodic_summaries_old`)
	}

	// Set version back to target.
	db.Exec("UPDATE schema_version SET version = ?", targetVersion)
	return db
}

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

// TestEnsureSchema_MigrationV11Only verifies the v11→12 patch (our new migration)
// applies correctly on a DB already at version 11.
func TestEnsureSchema_MigrationV11Only(t *testing.T) {
	db := openTestDB(t)

	// Apply fresh schema first, then roll version back to 11
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("initial EnsureSchema: %v", err)
	}
	db.Exec("UPDATE schema_version SET version = 11")

	// Re-apply — should run only migration 11→12
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema (v11→12) failed: %v", err)
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

// TestEnsureSchema_MigrationV11_SeedsAgentFiles verifies migration 11 seeds
// AGENTS_CORE.md and AGENTS_TASK.md and removes AGENTS_MINIMAL.md.
func TestEnsureSchema_MigrationV11_SeedsAgentFiles(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Use master tenant (seeded by EnsureSchema → seedMasterTenant)
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

	// Reset schema_version to 11 and re-apply migration
	db.Exec("UPDATE schema_version SET version = 11")
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema (re-apply v11): %v", err)
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

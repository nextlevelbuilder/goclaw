//go:build sqlite || sqliteonly

package sqlitestore

import (
	"database/sql"
	"strings"
	"testing"
)

// TestSQLiteSchemaUpgrade_25_to_26_HeartbeatFKSetNull verifies the v25→v26
// rebuild of agent_heartbeats:
//   - replaces FK clause with ON DELETE SET NULL
//   - preserves existing rows (id values) under FK toggle
//   - leaves heartbeat_run_logs FK references intact (resolved by table NAME)
//   - re-enables PRAGMA foreign_keys=ON after the rebuild
//   - successfully cascades a provider DELETE to NULL the heartbeat reference
func TestSQLiteSchemaUpgrade_25_to_26_HeartbeatFKSetNull(t *testing.T) {
	db := openTestDBAtVersion(t, 25)

	// At v25 the schema.sql was applied (which now has the v26 FK clause), then
	// version reset to 25. To simulate a real pre-v26 DB we must rebuild
	// agent_heartbeats with the OLD FK clause (no ON DELETE), and toggle FK off
	// for the rebuild itself.
	mustExec(t, db, "PRAGMA foreign_keys=OFF")
	mustExec(t, db, `CREATE TABLE agent_heartbeats_old (
		id TEXT NOT NULL PRIMARY KEY,
		agent_id TEXT NOT NULL UNIQUE REFERENCES agents(id) ON DELETE CASCADE,
		enabled BOOLEAN NOT NULL DEFAULT 0,
		interval_sec INT NOT NULL DEFAULT 1800,
		prompt TEXT,
		provider_id TEXT REFERENCES llm_providers(id),
		model VARCHAR(200),
		isolated_session BOOLEAN NOT NULL DEFAULT 1,
		light_context BOOLEAN NOT NULL DEFAULT 0,
		ack_max_chars INT NOT NULL DEFAULT 300,
		max_retries INT NOT NULL DEFAULT 2,
		active_hours_start VARCHAR(5),
		active_hours_end VARCHAR(5),
		timezone TEXT,
		channel VARCHAR(50),
		chat_id TEXT,
		next_run_at TEXT,
		last_run_at TEXT,
		last_status VARCHAR(20),
		last_error TEXT,
		run_count INT NOT NULL DEFAULT 0,
		suppress_count INT NOT NULL DEFAULT 0,
		metadata TEXT DEFAULT '{}',
		created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`)
	mustExec(t, db, `INSERT INTO agent_heartbeats_old SELECT * FROM agent_heartbeats`)
	mustExec(t, db, `DROP TABLE agent_heartbeats`)
	mustExec(t, db, `ALTER TABLE agent_heartbeats_old RENAME TO agent_heartbeats`)
	mustExec(t, db, "PRAGMA foreign_keys=ON")

	// Sanity: confirm we are now at the OLD FK shape.
	if got := tableSQL(t, db, "agent_heartbeats"); strings.Contains(got, "ON DELETE SET NULL") {
		t.Fatalf("expected OLD FK shape (no ON DELETE), got: %s", got)
	}

	// Seed: tenant + agent + provider + heartbeat referencing provider + a
	// heartbeat_run_log referencing the heartbeat. After the rebuild, the
	// run_log FK must still resolve to the renamed table.
	tenantID := "11111111-1111-1111-1111-111111111111"
	agentID := "22222222-2222-2222-2222-222222222222"
	providerID := "33333333-3333-3333-3333-333333333333"
	hbID := "44444444-4444-4444-4444-444444444444"
	logID := "55555555-5555-5555-5555-555555555555"
	mustExec(t, db, `INSERT INTO tenants (id, name, slug, status) VALUES (?, 'T', 't26', 'active')`, tenantID)
	mustExec(t, db, `INSERT INTO agents (id, agent_key, display_name, status, tenant_id, owner_id, model, provider, agent_type) VALUES (?, 'a', 'A', 'active', ?, 'o', 'm', 'p', 'predefined')`, agentID, tenantID)
	mustExec(t, db, `INSERT INTO llm_providers (id, name, provider_type, api_base, api_key, enabled, settings, tenant_id) VALUES (?, 'p26', 'openai-compat', 'http://x', 'k', 1, '{}', ?)`, providerID, tenantID)
	mustExec(t, db, `INSERT INTO agent_heartbeats (id, agent_id, enabled, provider_id, model) VALUES (?, ?, 1, ?, 'gpt-4')`, hbID, agentID, providerID)
	mustExec(t, db, `INSERT INTO heartbeat_run_logs (id, heartbeat_id, agent_id, status) VALUES (?, ?, ?, 'success')`, logID, hbID, agentID)

	// Run the migration v25 → v26.
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema (v25→26) failed: %v", err)
	}

	// Verify schema version bumped.
	var version int
	db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version)
	if version != SchemaVersion {
		t.Errorf("schema version = %d, want %d", version, SchemaVersion)
	}

	// Verify FK clause changed.
	if got := tableSQL(t, db, "agent_heartbeats"); !strings.Contains(got, "ON DELETE SET NULL") {
		t.Fatalf("expected FK ON DELETE SET NULL after migration, got: %s", got)
	}

	// Verify heartbeat row preserved (same id).
	var preservedID string
	if err := db.QueryRow(`SELECT id FROM agent_heartbeats WHERE id = ?`, hbID).Scan(&preservedID); err != nil {
		t.Fatalf("heartbeat row missing after rebuild: %v", err)
	}

	// Verify heartbeat_run_log FK still resolves to the renamed table.
	var logFKResolved string
	if err := db.QueryRow(
		`SELECT l.id FROM heartbeat_run_logs l JOIN agent_heartbeats h ON l.heartbeat_id = h.id WHERE l.id = ?`,
		logID,
	).Scan(&logFKResolved); err != nil {
		t.Fatalf("heartbeat_run_logs FK broken after rebuild: %v", err)
	}

	// Verify foreign_keys pragma is back to ON.
	var fkOn int
	db.QueryRow("PRAGMA foreign_keys").Scan(&fkOn)
	if fkOn != 1 {
		t.Errorf("PRAGMA foreign_keys = %d after migration, want 1 (ON)", fkOn)
	}

	// Verify ON DELETE SET NULL semantics: deleting the provider sets
	// heartbeat.provider_id NULL instead of erroring.
	if _, err := db.Exec(`DELETE FROM llm_providers WHERE id = ?`, providerID); err != nil {
		t.Fatalf("DELETE provider must succeed under SET NULL FK, got: %v", err)
	}
	var providerIDAfter *string
	db.QueryRow(`SELECT provider_id FROM agent_heartbeats WHERE id = ?`, hbID).Scan(&providerIDAfter)
	if providerIDAfter != nil {
		t.Fatalf("expected provider_id NULL after provider DELETE, got %v", *providerIDAfter)
	}

	// Verify foreign_key_check finds no violations.
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign_key_check reported violations after migration")
	}
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func tableSQL(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var sqlText string
	if err := db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&sqlText); err != nil {
		t.Fatalf("read table sql for %q: %v", name, err)
	}
	return sqlText
}

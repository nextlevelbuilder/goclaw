//go:build sqlite || sqliteonly

package sqlitestore

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
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

	for _, table := range []string{"hooks", "hook_agents"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatalf("lookup %s table: %v", table, err)
		}
		if count != 1 {
			t.Errorf("fresh schema missing %q table", table)
		}
	}
}

func TestEnsureSchema_PreHooksUpgradeCreatesHookTables(t *testing.T) {
	db := openTestDBAtVersion(t, 19)
	for _, table := range []string{"tenant_hook_budget", "hook_executions", "hook_agents", "hooks"} {
		if _, err := db.Exec(`DROP TABLE IF EXISTS ` + table); err != nil {
			t.Fatalf("drop %s: %v", table, err)
		}
	}
	if _, err := db.Exec(`UPDATE schema_version SET version = 19`); err != nil {
		t.Fatalf("set pre-hooks schema version: %v", err)
	}

	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema (pre-hooks to current) failed: %v", err)
	}

	for _, table := range []string{"hooks", "hook_agents"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatalf("lookup %s table: %v", table, err)
		}
		if count != 1 {
			t.Errorf("upgrade schema missing %q table", table)
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

func TestEnsureSchema_RejectsSchemaAheadVersion(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("initial EnsureSchema: %v", err)
	}

	ahead := SchemaVersion + 1
	if _, err := db.Exec(`UPDATE schema_version SET version = ?`, ahead); err != nil {
		t.Fatalf("set schema-ahead version: %v", err)
	}

	err := EnsureSchema(db)
	if err == nil {
		t.Fatal("EnsureSchema accepted a database schema newer than this binary")
	}
	want := fmt.Sprintf(
		"sqlite: database schema version %d is newer than supported version %d",
		ahead, SchemaVersion,
	)
	if err.Error() != want {
		t.Fatalf("EnsureSchema error = %q, want %q", err, want)
	}
}

func TestSQLiteWorkflowMigrationsTrueV58AndV59Predecessors(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	rebuildWorkflowTablesAsV58(t, db)

	for _, column := range []string{"workflow_id", "workflow_step_id", "workflow_kind", "notification_policy", "plan_revision"} {
		exists, err := sqliteColumnExists(db, "team_tasks", column)
		if err != nil {
			t.Fatalf("inspect v58 team_tasks.%s: %v", column, err)
		}
		if exists {
			t.Fatalf("v58 predecessor unexpectedly contains team_tasks.%s", column)
		}
	}
	for _, table := range []string{"team_workflows", "team_work_classification_audits"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatalf("inspect v58 table %s: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("v58 predecessor unexpectedly contains %s", table)
		}
	}

	tenantID, leadID, workerID, teamID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	genericTaskID := uuid.New()
	if _, err := db.Exec(`INSERT INTO tenants(id,name,slug,status,settings) VALUES(?,?,?,'active','{}')`, tenantID, "Tenant", "tenant-"+tenantID.String()); err != nil {
		t.Fatal(err)
	}
	for id, key := range map[uuid.UUID]string{leadID: "lead", workerID: "worker"} {
		if _, err := db.Exec(`INSERT INTO agents(id,agent_key,owner_id,provider,model,tenant_id) VALUES(?,?,?,'openai','test',?)`, id, key, "owner", tenantID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO agent_teams(id,name,lead_agent_id,status,settings,created_by,tenant_id) VALUES(?,?,?,'active','{}','owner',?)`, teamID, "Team", leadID, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO team_tasks(
		id,team_id,subject,description,status,owner_agent_id,priority,metadata,task_type,tenant_id
	) VALUES(?,?,?,'legacy task must survive','pending',?,7,'{"keep":"v58"}','general',?)`,
		genericTaskID, teamID, "Generic v58 task", workerID, tenantID); err != nil {
		t.Fatal(err)
	}

	if err := applySQLiteMigration(db, 58, migrations[58], false); err != nil {
		t.Fatalf("apply true v58 to v59 migration: %v", err)
	}
	assertSQLiteSchemaVersion(t, db, 59)
	var subject, description, metadata string
	var priority int
	if err := db.QueryRow(`SELECT subject,description,priority,metadata FROM team_tasks WHERE id=?`, genericTaskID).Scan(&subject, &description, &priority, &metadata); err != nil {
		t.Fatalf("read task after v58 to v59: %v", err)
	}
	if subject != "Generic v58 task" || description != "legacy task must survive" || priority != 7 || metadata != `{"keep":"v58"}` {
		t.Fatalf("v58 task changed: subject=%q description=%q priority=%d metadata=%q", subject, description, priority, metadata)
	}
	for _, column := range []string{"workflow_id", "workflow_step_id", "workflow_kind", "workflow_terminal", "dispatch_token", "dispatch_lease_until"} {
		exists, err := sqliteColumnExists(db, "team_tasks", column)
		if err != nil || !exists {
			t.Fatalf("v59 missing team_tasks.%s: exists=%v err=%v", column, exists, err)
		}
	}

	workflowID, workTaskID, auditTaskID := uuid.New(), uuid.New(), uuid.New()
	planHash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := db.Exec(`INSERT INTO team_workflows(
		id,team_id,tenant_id,status,canonical_plan,schema_version,plan_hash,
		coordinator_agent_id,coordinator_agent_key,origin_agent_id,origin_agent_key,
		origin_run_id,origin_session_key,origin_channel,origin_chat_id,origin_routing,
		expansion_token,finalize_token,failure_summary,result_summary,delivery_status
	) VALUES(?,?,?,'running','{"steps":["keep"]}',1,?,?,?,?,?,'run-v59','session-v59','ws','chat-v59','{"thread":"keep"}',
		'expand-v59','finalize-v59','failure-v59','result-v59','enqueuing')`,
		workflowID, teamID, tenantID, planHash, leadID, "lead", leadID, "lead"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO team_tasks(
		id,team_id,subject,status,owner_agent_id,metadata,task_type,workflow_id,workflow_step_id,workflow_kind,workflow_terminal,dispatch_token,tenant_id
	) VALUES(?,?,?,'in_progress',?,'{"keep":"work"}','general',?,'step-1','work',1,'dispatch-v59',?)`,
		workTaskID, teamID, "Workflow work", workerID, workflowID, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO team_tasks(
		id,team_id,subject,status,owner_agent_id,metadata,task_type,workflow_id,workflow_kind,tenant_id
	) VALUES(?,?,?,'pending',?,'{"keep":"audit"}','general',?,'audit',?)`,
		auditTaskID, teamID, "Workflow audit", leadID, workflowID, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE team_workflows SET audit_task_id=?,terminal_task_id=? WHERE id=?`, auditTaskID, workTaskID, workflowID); err != nil {
		t.Fatal(err)
	}

	if err := applySQLiteMigration(db, 59, migrations[59], false); err != nil {
		t.Fatalf("apply true v59 to v60 migration: %v", err)
	}
	assertSQLiteSchemaVersion(t, db, 60)
	for taskID, want := range map[uuid.UUID]string{
		workTaskID:    "workflow_internal",
		auditTaskID:   "workflow_internal",
		genericTaskID: "default",
	} {
		var got string
		if err := db.QueryRow(`SELECT notification_policy FROM team_tasks WHERE id=?`, taskID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("task %s policy = %q, want %q", taskID, got, want)
		}
	}
	var linkedWork, linkedAudit string
	if err := db.QueryRow(`SELECT terminal_task_id,audit_task_id FROM team_workflows WHERE id=?`, workflowID).Scan(&linkedWork, &linkedAudit); err != nil {
		t.Fatalf("read workflow links after v59 to v60: %v", err)
	}
	if linkedWork != workTaskID.String() || linkedAudit != auditTaskID.String() {
		t.Fatalf("workflow links changed: terminal=%q audit=%q", linkedWork, linkedAudit)
	}
	if _, err := db.Exec(`UPDATE team_tasks SET notification_policy='invalid' WHERE id=?`, genericTaskID); err == nil {
		t.Fatal("notification policy CHECK accepted an invalid value")
	}
	assertSQLiteForeignKeysClean(t, db)
}

func assertSQLiteSchemaVersion(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&got); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if got != want {
		t.Fatalf("schema version = %d, want %d", got, want)
	}
}

func rebuildWorkflowTablesAsV58(t *testing.T, db *sql.DB) {
	t.Helper()
	// Keep fixture DDL and the connection-local FK pragma on one connection.
	db.SetMaxOpenConns(1)
	mustExec(t, db, `PRAGMA foreign_keys=OFF`)
	mustExec(t, db, `DROP TRIGGER IF EXISTS trg_team_tasks_workflow_insert`)
	mustExec(t, db, `DROP TRIGGER IF EXISTS trg_team_tasks_workflow_update`)
	mustExec(t, db, `DROP TABLE team_tasks`)
	mustExec(t, db, `DROP TABLE team_workflows`)
	mustExec(t, db, `DROP TABLE team_work_classification_audits`)

	mustExec(t, db, `CREATE TABLE team_tasks (
		id TEXT NOT NULL PRIMARY KEY,
		team_id TEXT NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
		subject VARCHAR(500) NOT NULL,
		description TEXT,
		status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','dispatching','in_progress','completed','blocked','failed','in_review','cancelled','stale')),
		owner_agent_id TEXT REFERENCES agents(id) ON DELETE SET NULL,
		blocked_by TEXT NOT NULL DEFAULT '[]',
		priority INT NOT NULL DEFAULT 0,
		result TEXT,
		metadata TEXT NOT NULL DEFAULT '{}',
		user_id VARCHAR(255),
		channel VARCHAR(50),
		task_type VARCHAR(30) NOT NULL DEFAULT 'general',
		task_number INT NOT NULL DEFAULT 0,
		identifier VARCHAR(20),
		created_by_agent_id TEXT REFERENCES agents(id) ON DELETE SET NULL,
		assignee_user_id VARCHAR(255),
		parent_id TEXT REFERENCES team_tasks(id) ON DELETE SET NULL,
		chat_id VARCHAR(255) DEFAULT '',
		locked_at TEXT,
		lock_expires_at TEXT,
		progress_percent INT DEFAULT 0 CHECK (progress_percent BETWEEN 0 AND 100),
		progress_step TEXT,
		followup_at TEXT,
		followup_count INT NOT NULL DEFAULT 0,
		followup_max INT NOT NULL DEFAULT 0,
		followup_message TEXT,
		followup_channel VARCHAR(60),
		followup_chat_id VARCHAR(255),
		confidence_score REAL,
		comment_count INT NOT NULL DEFAULT 0,
		attachment_count INT NOT NULL DEFAULT 0,
		custom_scope TEXT,
		tenant_id TEXT NOT NULL REFERENCES tenants(id),
		created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`)
	for _, stmt := range []string{
		`CREATE INDEX idx_team_tasks_team ON team_tasks(team_id)`,
		`CREATE INDEX idx_team_tasks_status ON team_tasks(team_id,status)`,
		`CREATE INDEX idx_team_tasks_user_scope ON team_tasks(team_id,user_id) WHERE user_id IS NOT NULL`,
		`CREATE INDEX idx_tt_parent ON team_tasks(parent_id) WHERE parent_id IS NOT NULL`,
		`CREATE INDEX idx_tt_scope ON team_tasks(team_id,channel,chat_id)`,
		`CREATE INDEX idx_tt_type ON team_tasks(team_id,task_type)`,
		`CREATE INDEX idx_tt_lock ON team_tasks(lock_expires_at) WHERE lock_expires_at IS NOT NULL AND status='in_progress'`,
		`CREATE UNIQUE INDEX idx_tt_identifier ON team_tasks(team_id,identifier) WHERE identifier IS NOT NULL`,
		`CREATE INDEX idx_tt_followup ON team_tasks(followup_at) WHERE followup_at IS NOT NULL AND status='in_progress'`,
		`CREATE INDEX idx_tt_owner_status ON team_tasks(team_id,owner_agent_id,status)`,
		`CREATE INDEX idx_team_tasks_tenant ON team_tasks(tenant_id)`,
	} {
		mustExec(t, db, stmt)
	}
	mustExec(t, db, `UPDATE schema_version SET version=58`)
	mustExec(t, db, `PRAGMA foreign_keys=ON`)
	assertSQLiteForeignKeysClean(t, db)
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

// TestSQLiteSchemaUpgrade_23_to_24 verifies the v23→24 migration creates both
// scope-consistency triggers on an existing DB.
func TestSQLiteSchemaUpgrade_23_to_24(t *testing.T) {
	db := openTestDBAtVersion(t, 23)

	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema (v23→24) failed: %v", err)
	}

	var version int
	db.QueryRow("SELECT version FROM schema_version LIMIT 1").Scan(&version)
	if version != SchemaVersion {
		t.Errorf("schema version = %d, want %d", version, SchemaVersion)
	}

	// Verify both triggers exist in sqlite_master.
	for _, trigName := range []string{
		"trg_vault_docs_scope_consistency_ins",
		"trg_vault_docs_scope_consistency_upd",
	} {
		var count int
		db.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type='trigger' AND name=?`, trigName,
		).Scan(&count)
		if count != 1 {
			t.Errorf("trigger %q not found after migration", trigName)
		}
	}
}

// TestSQLiteVaultStore_UpsertTriggerEnforcesCheck verifies the v24 triggers
// fire on both the INSERT path and the UPDATE path (UPSERT ON CONFLICT).
func TestSQLiteVaultStore_UpsertTriggerEnforcesCheck(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	// Seed required FK rows: tenant + agent.
	tenantID := "00000000-0000-0000-0000-000000000001"
	agentID := "00000000-0000-0000-0000-000000000002"
	db.Exec(`INSERT INTO tenants (id, name, slug, status) VALUES (?, 'T', 't', 'active')`, tenantID)
	db.Exec(`INSERT INTO agents (id, agent_key, display_name, status, tenant_id, owner_id, model, provider)
		VALUES (?, 'agt', 'A', 'active', ?, 'owner', 'gpt-4o', 'openai')`, agentID, tenantID)

	// 1. Valid INSERT (personal + agent_id set) must succeed.
	_, err := db.Exec(
		`INSERT INTO vault_documents (id, tenant_id, agent_id, team_id, scope, path, path_basename, title, doc_type, content_hash)
		 VALUES ('doc-1', ?, ?, NULL, 'personal', '/a/b.md', 'b.md', 'T', 'note', 'h1')`,
		tenantID, agentID)
	if err != nil {
		t.Fatalf("valid INSERT failed: %v", err)
	}

	// 2. Invalid fresh INSERT (personal + agent_id NULL) must abort.
	_, err = db.Exec(
		`INSERT INTO vault_documents (id, tenant_id, agent_id, team_id, scope, path, path_basename, title, doc_type, content_hash)
		 VALUES ('doc-2', ?, NULL, NULL, 'personal', '/a/c.md', 'c.md', 'T2', 'note', 'h2')`,
		tenantID)
	if err == nil {
		t.Fatal("expected INSERT to fail scope_consistency check, but it succeeded")
	}

	// 3. UPSERT that would make scope inconsistent must abort on UPDATE path.
	_, err = db.Exec(
		`INSERT INTO vault_documents (id, tenant_id, agent_id, team_id, scope, path, path_basename, title, doc_type, content_hash)
		 VALUES ('doc-1', ?, NULL, NULL, 'personal', '/a/b.md', 'b.md', 'T-upd', 'note', 'h1')
		 ON CONFLICT(id) DO UPDATE SET agent_id = NULL, scope = 'personal'`,
		tenantID)
	if err == nil {
		t.Fatal("expected UPSERT to fail scope_consistency check on UPDATE path, but it succeeded")
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
	if targetVersion < 60 {
		if _, err := db.Exec(`ALTER TABLE team_tasks DROP COLUMN notification_policy`); err != nil {
			t.Fatalf("drop v60 notification_policy: %v", err)
		}
	}

	// Phase 03 (v15 → v16) adds:
	//   - team_task_attachments.base_name
	//   - vault_documents.path_basename
	//   - vault_links.metadata
	// Strip these when the test targets any version < 16 so the v13 migration
	// (which recreates vault_documents via SELECT *) doesn't hit a column
	// count mismatch.
	if targetVersion < 16 {
		// Recreate vault_documents without path_basename.
		db.Exec(`CREATE TABLE vault_documents_v15 AS SELECT
			id, tenant_id, agent_id, team_id, scope, custom_scope, path,
			title, doc_type, content_hash, summary, metadata,
			created_at, updated_at
			FROM vault_documents`)
		db.Exec(`DROP TABLE vault_documents`)
		db.Exec(`CREATE TABLE vault_documents (
			id TEXT NOT NULL PRIMARY KEY,
			tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
			agent_id TEXT REFERENCES agents(id) ON DELETE SET NULL,
			team_id TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
			scope TEXT NOT NULL DEFAULT 'personal',
			custom_scope TEXT,
			path TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			doc_type TEXT NOT NULL DEFAULT 'note',
			content_hash TEXT NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			metadata TEXT DEFAULT '{}',
			created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		)`)
		db.Exec(`INSERT INTO vault_documents SELECT * FROM vault_documents_v15`)
		db.Exec(`DROP TABLE vault_documents_v15`)

		// Recreate team_task_attachments without base_name.
		db.Exec(`CREATE TABLE team_task_attachments_v15 AS SELECT
			id, task_id, team_id, chat_id, path, file_size, mime_type,
			created_by_agent_id, created_by_sender_id, metadata, custom_scope,
			tenant_id, created_at
			FROM team_task_attachments`)
		db.Exec(`DROP TABLE team_task_attachments`)
		db.Exec(`CREATE TABLE team_task_attachments (
			id TEXT NOT NULL PRIMARY KEY,
			task_id TEXT NOT NULL REFERENCES team_tasks(id) ON DELETE CASCADE,
			team_id TEXT NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
			chat_id VARCHAR(255) NOT NULL DEFAULT '',
			path TEXT NOT NULL,
			file_size BIGINT NOT NULL DEFAULT 0,
			mime_type VARCHAR(100) DEFAULT '',
			created_by_agent_id TEXT REFERENCES agents(id),
			created_by_sender_id VARCHAR(255) DEFAULT '',
			metadata TEXT NOT NULL DEFAULT '{}',
			custom_scope TEXT,
			tenant_id TEXT NOT NULL REFERENCES tenants(id),
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			UNIQUE(task_id, path)
		)`)
		db.Exec(`INSERT INTO team_task_attachments SELECT * FROM team_task_attachments_v15`)
		db.Exec(`DROP TABLE team_task_attachments_v15`)

		// Recreate vault_links without metadata column.
		db.Exec(`CREATE TABLE vault_links_v15 AS SELECT
			id, from_doc_id, to_doc_id, link_type, context, custom_scope, created_at
			FROM vault_links`)
		db.Exec(`DROP TABLE vault_links`)
		db.Exec(`CREATE TABLE vault_links (
			id TEXT NOT NULL PRIMARY KEY,
			from_doc_id TEXT NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
			to_doc_id TEXT NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
			link_type TEXT NOT NULL DEFAULT 'wikilink',
			context TEXT NOT NULL DEFAULT '',
			custom_scope TEXT,
			created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			UNIQUE(from_doc_id, to_doc_id, link_type)
		)`)
		db.Exec(`INSERT INTO vault_links SELECT * FROM vault_links_v15`)
		db.Exec(`DROP TABLE vault_links_v15`)
	}

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

	if targetVersion < 25 {
		// Migration 24→25 adds vault_documents.chat_id + idx_vault_docs_team_chat.
		// Drop both so the migration's ALTER TABLE / CREATE INDEX succeed.
		db.Exec(`DROP INDEX IF EXISTS idx_vault_docs_team_chat`)
		db.Exec(`ALTER TABLE vault_documents DROP COLUMN chat_id`)
	}

	// Set version back to target.
	db.Exec("UPDATE schema_version SET version = ?", targetVersion)
	return db
}

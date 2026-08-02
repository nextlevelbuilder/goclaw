//go:build sqlite || sqliteonly

package sqlitestore

import (
	"database/sql"
	"strings"
	"testing"
)

func TestEnsureSchema_FreshWorkflowEnforcementEndpoint(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	assertWorkflowEnforcementEndpoint(t, db)
	assertSQLitePlanHashDomains(t, db)
	assertSQLiteClassifierAuditDomains(t, db)
	assertSQLiteForeignKeysClean(t, db)
}

func TestEnsureSchema_MigrationV61PopulatedWorkflowPredecessor(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	rebuildWorkflowTablesAsV61(t, db)

	for _, column := range []string{
		"plan_revision", "dispatch_count", "blocker_reason", "recovery_count",
		"escalation_status", "escalation_attempt_count", "escalation_next_at", "escalation_last_error",
	} {
		exists, err := sqliteColumnExists(db, "team_tasks", column)
		if err != nil {
			t.Fatalf("inspect predecessor column %s: %v", column, err)
		}
		if exists {
			t.Fatalf("v61 predecessor unexpectedly contains team_tasks.%s", column)
		}
	}
	if got := tableSQL(t, db, "team_workflows"); strings.Contains(got, "needs_revision") || strings.Contains(got, "classification_audit_id") {
		t.Fatalf("team_workflows is not the v61 predecessor shape: %s", got)
	}

	const (
		tenantID    = "11111111-1111-1111-1111-111111111111"
		leadID      = "22222222-2222-2222-2222-222222222222"
		workerID    = "33333333-3333-3333-3333-333333333333"
		teamID      = "44444444-4444-4444-4444-444444444444"
		workflowID  = "55555555-5555-5555-5555-555555555555"
		workTaskID  = "66666666-6666-6666-6666-666666666666"
		auditTaskID = "77777777-7777-7777-7777-777777777777"
	)
	mustExec(t, db, `INSERT INTO tenants(id,name,slug,status,settings) VALUES(?, 'Tenant', 'workflow-v61', 'active', '{}')`, tenantID)
	mustExec(t, db, `INSERT INTO agents(id,agent_key,display_name,status,owner_id,provider,model,agent_type,tenant_id)
		VALUES(?, 'lead', 'Lead', 'active', 'owner', 'openai', 'test', 'predefined', ?)`, leadID, tenantID)
	mustExec(t, db, `INSERT INTO agents(id,agent_key,display_name,status,owner_id,provider,model,agent_type,tenant_id)
		VALUES(?, 'worker', 'Worker', 'active', 'owner', 'openai', 'test', 'predefined', ?)`, workerID, tenantID)
	mustExec(t, db, `INSERT INTO agent_teams(id,name,lead_agent_id,status,settings,created_by,tenant_id)
		VALUES(?, 'Team', ?, 'active', '{}', 'owner', ?)`, teamID, leadID, tenantID)

	mustExec(t, db, `INSERT INTO team_workflows(
		id,team_id,tenant_id,status,canonical_plan,schema_version,plan_hash,
		coordinator_agent_id,coordinator_agent_key,origin_agent_id,origin_agent_key,
		origin_run_id,origin_session_key,origin_channel,origin_chat_id,
		origin_peer_kind,origin_local_key,origin_user_id,origin_sender_id,origin_role,
		origin_routing,auto_expand,expansion_token,expansion_lease_until,
		finalize_token,finalize_lease_until,finalize_claimed_at,finalized_at,
		failure_settle_deadline,failure_summary,result_summary,delivery_status,
		delivery_token,delivery_lease_until,delivered_at,created_at,updated_at
	) VALUES(
		?,?,?,'running','{"steps":["preserve"]}',7,?,
		?,'lead',?,'lead','run-v61','session-v61','ws','chat-v61',
		'group','local-v61','user-v61','sender-v61','member',
		'{"thread":"keep"}',1,'expand-token','2030-01-01T00:00:00Z',
		'finalize-token','2030-01-02T00:00:00Z','2029-12-01T00:00:00Z',NULL,
		'2030-01-03T00:00:00Z','failure-keep','result-keep','enqueuing',
		'delivery-token','2030-01-04T00:00:00Z',NULL,'2029-01-01T00:00:00Z','2029-01-02T00:00:00Z'
	)`, workflowID, teamID, tenantID, strings.Repeat("a", 64), leadID, leadID)

	mustExec(t, db, `INSERT INTO team_tasks(
		id,team_id,subject,description,status,owner_agent_id,metadata,task_type,
		workflow_id,workflow_step_id,workflow_kind,workflow_terminal,
		dispatch_token,dispatch_lease_until,notification_policy,tenant_id
	) VALUES(?,?,'Work step','must survive','in_progress',?,
		'{"dispatch_count":3,"keep":"yes"}','general',?, 'step-1','work',1,
		'dispatch-token','2030-02-01T00:00:00Z','workflow_internal',?)`,
		workTaskID, teamID, workerID, workflowID, tenantID)
	mustExec(t, db, `INSERT INTO team_tasks(
		id,team_id,subject,status,owner_agent_id,metadata,task_type,
		workflow_id,workflow_kind,workflow_terminal,notification_policy,tenant_id
	) VALUES(?,?,'Audit task','pending',?,'{"keep":"audit"}','general',?,'audit',0,'workflow_internal',?)`,
		auditTaskID, teamID, leadID, workflowID, tenantID)
	mustExec(t, db, `UPDATE team_workflows SET audit_task_id=?, terminal_task_id=? WHERE id=?`, auditTaskID, workTaskID, workflowID)

	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema (v61 to v62): %v", err)
	}

	assertWorkflowEnforcementEndpoint(t, db)
	assertSQLitePlanHashDomains(t, db)
	assertSQLiteClassifierAuditDomains(t, db)

	var (
		status, canonicalPlan, planHash, routing, expansionToken, finalizeToken string
		failureSummary, resultSummary, deliveryStatus, deliveryToken            string
		planRevision, expansionAttempts, deliveryAttempts                       int
	)
	if err := db.QueryRow(`SELECT status,canonical_plan,plan_hash,origin_routing,expansion_token,
		finalize_token,failure_summary,result_summary,delivery_status,delivery_token,
		plan_revision,expansion_attempt_count,delivery_attempt_count
		FROM team_workflows WHERE id=?`, workflowID).Scan(
		&status, &canonicalPlan, &planHash, &routing, &expansionToken, &finalizeToken,
		&failureSummary, &resultSummary, &deliveryStatus, &deliveryToken,
		&planRevision, &expansionAttempts, &deliveryAttempts,
	); err != nil {
		t.Fatalf("read upgraded workflow: %v", err)
	}
	if status != "running" || canonicalPlan != `{"steps":["preserve"]}` ||
		planHash != strings.Repeat("a", 64) || routing != `{"thread":"keep"}` ||
		expansionToken != "expand-token" || finalizeToken != "finalize-token" ||
		failureSummary != "failure-keep" || resultSummary != "result-keep" ||
		deliveryStatus != "enqueuing" || deliveryToken != "delivery-token" {
		t.Fatalf("workflow data changed during rebuild: status=%q plan=%q hash=%q routing=%q expansion=%q finalize=%q failure=%q result=%q delivery=%q token=%q",
			status, canonicalPlan, planHash, routing, expansionToken, finalizeToken,
			failureSummary, resultSummary, deliveryStatus, deliveryToken)
	}
	if planRevision != 1 || expansionAttempts != 0 || deliveryAttempts != 0 {
		t.Fatalf("workflow enforcement defaults = revision %d expansion %d delivery %d", planRevision, expansionAttempts, deliveryAttempts)
	}

	var taskID, dispatchToken, notificationPolicy, metadata string
	var dispatchCount, taskRevision int
	if err := db.QueryRow(`SELECT id,dispatch_token,notification_policy,metadata,dispatch_count,plan_revision
		FROM team_tasks WHERE id=?`, workTaskID).Scan(
		&taskID, &dispatchToken, &notificationPolicy, &metadata, &dispatchCount, &taskRevision,
	); err != nil {
		t.Fatalf("read upgraded task: %v", err)
	}
	if taskID != workTaskID || dispatchToken != "dispatch-token" || notificationPolicy != "workflow_internal" {
		t.Fatalf("task linkage data changed: id=%q token=%q policy=%q", taskID, dispatchToken, notificationPolicy)
	}
	if dispatchCount != 3 || taskRevision != 1 || metadata != `{"keep":"yes"}` {
		t.Fatalf("task backfill = count %d revision %d metadata %q", dispatchCount, taskRevision, metadata)
	}

	// The revision-aware key permits the same step in a later revision but not
	// a duplicate within that revision.
	mustExec(t, db, `INSERT INTO team_tasks(
		id,team_id,subject,status,owner_agent_id,task_type,workflow_id,workflow_step_id,
		workflow_kind,workflow_terminal,plan_revision,notification_policy,tenant_id
	) VALUES('88888888-8888-8888-8888-888888888888',?,'Revision 2','pending',?,
		'general',?,'step-1','work',0,2,'workflow_internal',?)`, teamID, leadID, workflowID, tenantID)
	if _, err := db.Exec(`INSERT INTO team_tasks(
		id,team_id,subject,status,owner_agent_id,task_type,workflow_id,workflow_step_id,
		workflow_kind,workflow_terminal,plan_revision,notification_policy,tenant_id
	) VALUES('99999999-9999-9999-9999-999999999999',?,'Duplicate revision 2','pending',NULL,
		'general',?,'step-1','work',0,2,'workflow_internal',?)`, teamID, workflowID, tenantID); err == nil {
		t.Fatal("revision-aware workflow-step index accepted a duplicate revision")
	}
	if _, err := db.Exec(`UPDATE team_tasks SET status='in_progress', owner_agent_id=?
		WHERE id='88888888-8888-8888-8888-888888888888'`, workerID); err == nil {
		t.Fatal("active-owner index accepted a second active work task")
	}

	// Widened domains and classifier-audit FK are observable on upgraded DBs.
	mustExec(t, db, `UPDATE team_workflows SET status='needs_revision', delivery_status='dead' WHERE id=?`, workflowID)
	const auditID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	mustExec(t, db, `INSERT INTO team_work_classification_audits(id,tenant_id,ingress,run_id,agent_id)
		VALUES(?,?,'ws','run-audit',?)`, auditID, tenantID, leadID)
	mustExec(t, db, `UPDATE team_workflows SET classification_audit_id=? WHERE id=?`, auditID, workflowID)
	mustExec(t, db, `DELETE FROM team_work_classification_audits WHERE id=?`, auditID)
	var classificationAuditID *string
	if err := db.QueryRow(`SELECT classification_audit_id FROM team_workflows WHERE id=?`, workflowID).Scan(&classificationAuditID); err != nil {
		t.Fatalf("read classification audit FK: %v", err)
	}
	if classificationAuditID != nil {
		t.Fatalf("classification_audit_id = %q after audit delete, want NULL", *classificationAuditID)
	}

	assertSQLiteForeignKeysClean(t, db)
}

func TestEnsureSchema_ClassifierAuditDomains(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	assertSQLiteClassifierAuditDomains(t, db)
}

func assertSQLitePlanHashDomains(t *testing.T, db *sql.DB) {
	t.Helper()
	tenantID := "plan-hash-domains-" + strings.ReplaceAll(t.Name(), "/", "-")
	agentID := tenantID + "-agent"
	teamID := tenantID + "-team"
	mustExec(t, db, `INSERT INTO tenants(id,name,slug,status) VALUES(?,'Tenant',?,'active')`, tenantID, tenantID)
	mustExec(t, db, `INSERT INTO agents(id,agent_key,owner_id,provider,model,tenant_id)
		VALUES(?,'agent','owner','openai','test',?)`, agentID, tenantID)
	mustExec(t, db, `INSERT INTO agent_teams(id,name,lead_agent_id,status,settings,created_by,tenant_id)
		VALUES(?,'Team',?,'active','{}','owner',?)`, teamID, agentID, tenantID)

	insert := func(id, hash string) error {
		_, err := db.Exec(`INSERT INTO team_workflows(
			id,team_id,tenant_id,status,canonical_plan,schema_version,plan_hash,
			coordinator_agent_id,coordinator_agent_key,origin_agent_id,origin_agent_key,
			origin_run_id,origin_session_key,origin_channel,origin_chat_id
		) VALUES(?,?,?,'running','{}',1,?,?,'agent',?,'agent',?,?, 'ws','chat')`,
			id, teamID, tenantID, hash, agentID, agentID, id+"-run", id+"-session")
		return err
	}
	if err := insert(tenantID+"-valid", strings.Repeat("a", 64)); err != nil {
		t.Fatalf("workflow rejected valid lowercase plan hash: %v", err)
	}
	for name, hash := range map[string]string{
		"uppercase":    strings.Repeat("A", 64),
		"non-hex":      strings.Repeat("g", 64),
		"wrong length": strings.Repeat("a", 63),
	} {
		if err := insert(tenantID+"-"+strings.ReplaceAll(name, " ", "-"), hash); err == nil {
			t.Fatalf("workflow accepted %s plan hash", name)
		}
	}
}

func assertSQLiteClassifierAuditDomains(t *testing.T, db *sql.DB) {
	t.Helper()
	tenantID := "audit-domains-" + strings.ReplaceAll(t.Name(), "/", "-")
	mustExec(t, db, `INSERT INTO tenants(id,name,slug,status) VALUES(?,'Tenant',?,'active')`, tenantID, tenantID)
	mustExec(t, db, `INSERT INTO team_work_classification_audits(
		id,tenant_id,ingress,traits,requested_mode,effective_mode,independent_review,stage_statuses
	) VALUES(?,?,'ws','["atomic"]','self','self',1,'{"shape":"passed"}')`, tenantID+"-valid", tenantID)
	for name, testCase := range map[string]struct {
		columns string
		values  string
		value   any
	}{
		"invalid ingress":             {"ingress", "?", "invalid"},
		"invalid requested mode":      {"ingress,requested_mode", "'ws',?", "invalid"},
		"invalid effective mode":      {"ingress,effective_mode", "'ws',?", "invalid"},
		"invalid traits JSON":         {"ingress,traits", "'ws',?", `not-json`},
		"invalid stage statuses JSON": {"ingress,stage_statuses", "'ws',?", `not-json`},
		"invalid boolean":             {"ingress,independent_review", "'ws',?", 2},
	} {
		_, err := db.Exec(`INSERT INTO team_work_classification_audits(id,tenant_id,`+testCase.columns+`)
			VALUES(?,?,`+testCase.values+`)`, tenantID+"-"+strings.ReplaceAll(name, " ", "-"), tenantID, testCase.value)
		if err == nil {
			t.Fatalf("classifier audit accepted %s", name)
		}
	}
}

func TestEnsureSchema_MigrationV61RejectsDuplicateActiveOwners(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	rebuildWorkflowTablesAsV61(t, db)

	const (
		tenantID   = "10111111-1111-1111-1111-111111111111"
		leadID     = "10222222-2222-2222-2222-222222222222"
		workerID   = "10333333-3333-3333-3333-333333333333"
		teamID     = "10444444-4444-4444-4444-444444444444"
		workflowID = "10555555-5555-5555-5555-555555555555"
	)
	mustExec(t, db, `INSERT INTO tenants(id,name,slug,status) VALUES(?,'Tenant','duplicate-owner-v61','active')`, tenantID)
	mustExec(t, db, `INSERT INTO agents(id,agent_key,owner_id,provider,model,tenant_id) VALUES(?,'lead','owner','openai','test',?)`, leadID, tenantID)
	mustExec(t, db, `INSERT INTO agents(id,agent_key,owner_id,provider,model,tenant_id) VALUES(?,'worker','owner','openai','test',?)`, workerID, tenantID)
	mustExec(t, db, `INSERT INTO agent_teams(id,name,lead_agent_id,status,settings,created_by,tenant_id) VALUES(?,'Team',?,'active','{}','owner',?)`, teamID, leadID, tenantID)
	mustExec(t, db, `INSERT INTO team_workflows(
		id,team_id,tenant_id,status,canonical_plan,schema_version,plan_hash,
		coordinator_agent_id,coordinator_agent_key,origin_agent_id,origin_agent_key,
		origin_run_id,origin_session_key,origin_channel,origin_chat_id
	) VALUES(?,?,?,'running','{}',1,?,?,'lead',?,'lead','run','session','ws','chat')`,
		workflowID, teamID, tenantID, strings.Repeat("b", 64), leadID, leadID)
	for _, taskID := range []string{
		"10666666-6666-6666-6666-666666666666",
		"10777777-7777-7777-7777-777777777777",
	} {
		mustExec(t, db, `INSERT INTO team_tasks(
			id,team_id,subject,status,owner_agent_id,task_type,workflow_id,workflow_step_id,
			workflow_kind,notification_policy,tenant_id
		) VALUES(?,?,'Active','in_progress',?,'general',?,?,'work','workflow_internal',?)`,
			taskID, teamID, workerID, workflowID, taskID, tenantID)
	}

	if err := EnsureSchema(db); err == nil {
		t.Fatal("v61 migration silently accepted duplicate active workflow owners")
	}
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("read schema version after rejected migration: %v", err)
	}
	if version != 62 {
		t.Fatalf("schema version = %d after rejected migration, want 62", version)
	}
}

func rebuildWorkflowTablesAsV61(t *testing.T, db *sql.DB) {
	t.Helper()
	// Keep fixture DDL and the connection-local FK pragma on one connection.
	db.SetMaxOpenConns(1)
	mustExec(t, db, `PRAGMA foreign_keys=OFF`)

	for _, index := range []string{
		"idx_team_tasks_workflow_step", "idx_team_tasks_workflow_status",
		"idx_team_tasks_dispatch_recovery", "idx_team_tasks_active_owner",
		"idx_team_workflows_creation", "idx_team_workflows_plan_lookup",
		"idx_team_workflows_recovery", "idx_team_workflows_delivery_recovery",
	} {
		mustExec(t, db, `DROP INDEX IF EXISTS `+index)
	}
	mustExec(t, db, `DROP TRIGGER IF EXISTS trg_team_tasks_workflow_insert`)
	mustExec(t, db, `DROP TRIGGER IF EXISTS trg_team_tasks_workflow_update`)
	mustExec(t, db, `DROP TABLE team_tasks`)
	mustExec(t, db, `DROP TABLE team_workflows`)
	mustExec(t, db, `DROP TABLE team_work_classification_audits`)

	mustExec(t, db, `CREATE TABLE team_workflows (
		id TEXT NOT NULL PRIMARY KEY,
		team_id TEXT NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
		tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
		status VARCHAR(24) NOT NULL CHECK (status IN ('pending_expansion','running','failing','completed','failed')),
		canonical_plan TEXT NOT NULL,
		schema_version INTEGER NOT NULL,
		plan_hash VARCHAR(64) NOT NULL CHECK (length(plan_hash) = 64),
		coordinator_agent_id TEXT NOT NULL REFERENCES agents(id),
		coordinator_agent_key VARCHAR(255) NOT NULL,
		origin_agent_id TEXT NOT NULL REFERENCES agents(id),
		origin_agent_key VARCHAR(255) NOT NULL,
		origin_run_id VARCHAR(255) NOT NULL,
		origin_session_key VARCHAR(500) NOT NULL,
		origin_channel VARCHAR(60) NOT NULL,
		origin_chat_id VARCHAR(255) NOT NULL,
		origin_peer_kind VARCHAR(20) NOT NULL DEFAULT 'direct',
		origin_local_key VARCHAR(500) NOT NULL DEFAULT '',
		origin_user_id VARCHAR(255) NOT NULL DEFAULT '',
		origin_sender_id VARCHAR(255) NOT NULL DEFAULT '',
		origin_role VARCHAR(60) NOT NULL DEFAULT '',
		origin_routing TEXT NOT NULL DEFAULT '{}',
		auto_expand INTEGER NOT NULL DEFAULT 0,
		audit_task_id TEXT REFERENCES team_tasks(id) ON DELETE SET NULL,
		terminal_task_id TEXT REFERENCES team_tasks(id) ON DELETE SET NULL,
		expansion_token TEXT,
		expansion_lease_until TEXT,
		finalize_token TEXT,
		finalize_lease_until TEXT,
		finalize_claimed_at TEXT,
		finalized_at TEXT,
		failure_settle_deadline TEXT,
		failure_summary TEXT NOT NULL DEFAULT '',
		result_summary TEXT NOT NULL DEFAULT '',
		delivery_status VARCHAR(16) NOT NULL DEFAULT 'pending' CHECK (delivery_status IN ('pending','enqueuing','delivered')),
		delivery_token TEXT,
		delivery_lease_until TEXT,
		delivered_at TEXT,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
	)`)
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
		workflow_id TEXT REFERENCES team_workflows(id) ON DELETE CASCADE,
		workflow_step_id VARCHAR(100),
		workflow_kind VARCHAR(10),
		workflow_terminal INTEGER NOT NULL DEFAULT 0,
		dispatch_token TEXT,
		dispatch_lease_until TEXT,
		notification_policy VARCHAR(24) NOT NULL DEFAULT 'default' CHECK (notification_policy IN ('default','suppress_handoff','workflow_internal')),
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

	mustExec(t, db, `CREATE UNIQUE INDEX idx_team_tasks_workflow_step ON team_tasks(tenant_id,workflow_id,workflow_step_id) WHERE workflow_id IS NOT NULL`)
	mustExec(t, db, `CREATE INDEX idx_team_tasks_workflow_status ON team_tasks(tenant_id,workflow_id,status) WHERE workflow_id IS NOT NULL`)
	mustExec(t, db, `CREATE INDEX idx_team_tasks_dispatch_recovery ON team_tasks(tenant_id,status,dispatch_lease_until) WHERE workflow_id IS NOT NULL AND workflow_kind='work'`)
	mustExec(t, db, `CREATE UNIQUE INDEX idx_team_workflows_creation ON team_workflows(tenant_id,team_id,origin_run_id,plan_hash)`)
	mustExec(t, db, `CREATE INDEX idx_team_workflows_plan_lookup ON team_workflows(tenant_id,team_id,plan_hash,status)`)
	mustExec(t, db, `CREATE INDEX idx_team_workflows_recovery ON team_workflows(tenant_id,status,expansion_lease_until,finalize_lease_until)`)
	mustExec(t, db, `CREATE INDEX idx_team_workflows_delivery_recovery ON team_workflows(tenant_id,delivery_status,delivery_lease_until) WHERE finalized_at IS NOT NULL AND delivered_at IS NULL`)
	mustExec(t, db, `CREATE TRIGGER trg_team_tasks_workflow_insert BEFORE INSERT ON team_tasks
		WHEN NOT (
			(NEW.workflow_id IS NULL AND NEW.workflow_step_id IS NULL AND NEW.workflow_kind IS NULL AND NEW.workflow_terminal=0 AND NEW.dispatch_token IS NULL AND NEW.dispatch_lease_until IS NULL)
			OR (NEW.workflow_id IS NOT NULL AND NEW.workflow_kind='audit' AND NEW.workflow_step_id IS NULL AND NEW.workflow_terminal=0 AND NEW.dispatch_token IS NULL AND NEW.dispatch_lease_until IS NULL)
			OR (NEW.workflow_id IS NOT NULL AND NEW.workflow_kind='work' AND NEW.workflow_step_id IS NOT NULL)
		) BEGIN SELECT RAISE(ABORT,'invalid workflow task fields'); END`)
	mustExec(t, db, `CREATE TRIGGER trg_team_tasks_workflow_update BEFORE UPDATE OF workflow_id,workflow_step_id,workflow_kind,workflow_terminal,dispatch_token,dispatch_lease_until ON team_tasks
		WHEN NOT (
			(NEW.workflow_id IS NULL AND NEW.workflow_step_id IS NULL AND NEW.workflow_kind IS NULL AND NEW.workflow_terminal=0 AND NEW.dispatch_token IS NULL AND NEW.dispatch_lease_until IS NULL)
			OR (NEW.workflow_id IS NOT NULL AND NEW.workflow_kind='audit' AND NEW.workflow_step_id IS NULL AND NEW.workflow_terminal=0 AND NEW.dispatch_token IS NULL AND NEW.dispatch_lease_until IS NULL)
			OR (NEW.workflow_id IS NOT NULL AND NEW.workflow_kind='work' AND NEW.workflow_step_id IS NOT NULL)
		) BEGIN SELECT RAISE(ABORT,'invalid workflow task fields'); END`)
	mustExec(t, db, `UPDATE schema_version SET version=61`)
	mustExec(t, db, `PRAGMA foreign_keys=ON`)

	var fkOn int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fkOn); err != nil {
		t.Fatalf("read predecessor foreign_keys: %v", err)
	}
	if fkOn != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d after v61 fixture rebuild, want 1", fkOn)
	}
}

func assertWorkflowEnforcementEndpoint(t *testing.T, db *sql.DB) {
	t.Helper()
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != SchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, SchemaVersion)
	}
	for _, column := range []string{
		"plan_revision", "dispatch_count", "blocker_reason", "recovery_count",
		"escalation_status", "escalation_attempt_count", "escalation_next_at", "escalation_last_error",
	} {
		exists, err := sqliteColumnExists(db, "team_tasks", column)
		if err != nil {
			t.Fatalf("inspect endpoint column %s: %v", column, err)
		}
		if !exists {
			t.Fatalf("endpoint missing team_tasks.%s", column)
		}
	}
	workflowSQL := tableSQL(t, db, "team_workflows")
	for _, fragment := range []string{"needs_revision", "cancelling", "cancelled", "'dead'", "classification_audit_id"} {
		if !strings.Contains(workflowSQL, fragment) {
			t.Fatalf("endpoint team_workflows missing %q: %s", fragment, workflowSQL)
		}
	}
	auditSQL := tableSQL(t, db, "team_work_classification_audits")
	for _, fragment := range []string{
		"json_valid(traits)", "independent_review IN (0,1)", "json_valid(stage_statuses)",
	} {
		if !strings.Contains(auditSQL, fragment) {
			t.Fatalf("endpoint classifier audit missing %q: %s", fragment, auditSQL)
		}
	}
	for index, fragments := range map[string][]string{
		"idx_team_tasks_workflow_step": {"tenant_id", "workflow_id", "plan_revision", "workflow_step_id"},
		"idx_team_tasks_active_owner":  {"tenant_id", "owner_agent_id", "workflow_kind = 'work'", "dispatching", "in_progress"},
		"idx_twc_audits_tenant_time":   {"tenant_id", "created_at DESC"},
	} {
		var indexSQL string
		if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='index' AND name=?`, index).Scan(&indexSQL); err != nil {
			t.Fatalf("inspect endpoint index %s: %v", index, err)
		}
		for _, fragment := range fragments {
			if !strings.Contains(indexSQL, fragment) {
				t.Fatalf("endpoint index %s missing %q: %s", index, fragment, indexSQL)
			}
		}
	}
}

func assertSQLiteForeignKeysClean(t *testing.T, db *sql.DB) {
	t.Helper()
	var fkOn int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fkOn); err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if fkOn != 1 {
		t.Fatalf("PRAGMA foreign_keys = %d, want 1", fkOn)
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatalf("foreign_key_check: %v", err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign_key_check reported violations")
	}
}

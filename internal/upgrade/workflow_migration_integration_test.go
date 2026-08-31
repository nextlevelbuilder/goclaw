//go:build integration

package upgrade

import (
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestPostgresWorkflowMigrationPopulatedUpgradeAndRollback(t *testing.T) {
	dsn := postgresWorkflowMigrationTestDSN(t)
	schema := "phase9_workflow_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	scopedDSN := postgresDSNWithSearchPath(t, dsn, schema)

	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open disposable PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	if err := adminDB.Ping(); err != nil {
		t.Skipf("disposable PostgreSQL is not reachable: %v", err)
	}
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminDB.Exec("CREATE SCHEMA " + quotedSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminDB.Exec("DROP SCHEMA IF EXISTS " + quotedSchema + " CASCADE"); err != nil {
			t.Errorf("drop isolated migration schema: %v", err)
		}
	})

	db, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		t.Fatalf("open isolated migration schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	m, err := migrate.New("file://../../migrations", scopedDSN)
	if err != nil {
		t.Fatalf("create migration runner: %v", err)
	}
	t.Cleanup(func() {
		_, _ = m.Close()
	})

	if err := m.Migrate(95); err != nil {
		t.Fatalf("migrate isolated schema to v95: %v", err)
	}
	assertPostgresMigrationVersion(t, db, 95, false)
	t.Log("applied true PostgreSQL predecessor v95")

	// v96 is upstream's subagent root-agent scope (not Team Work). It must apply
	// cleanly ahead of the Team Work chain.
	if err := m.Steps(1); err != nil {
		t.Fatalf("apply upstream subagent scope migration v96: %v", err)
	}
	assertPostgresMigrationVersion(t, db, 96, false)

	// v97 is the Team Work workflows base table.
	if err := m.Steps(1); err != nil {
		t.Fatalf("apply workflow migration v97: %v", err)
	}
	assertPostgresMigrationVersion(t, db, 97, false)

	ids := seedPostgresWorkflowV97(t, db)

	// v98 is the notification-policy migration.
	if err := m.Steps(1); err != nil {
		t.Fatalf("apply notification-policy migration v98: %v", err)
	}
	assertPostgresTaskPolicy(t, db, ids.workflowTask, "workflow_internal")
	assertPostgresTaskPolicy(t, db, ids.genericTask, "default")

	// v99 is enforcement and equals RequiredSchemaVersion.
	if err := m.Steps(1); err != nil {
		t.Fatalf("apply enforcement migration v99: %v", err)
	}
	assertPostgresMigrationVersion(t, db, RequiredSchemaVersion, false)
	assertPostgresWorkflowEndpoint(t, db, ids)
	t.Log("populated PostgreSQL v95→v99 upgrade preserved and backfilled workflow state")

	// Compatible v99 state must make a clean one-step rollback and re-upgrade.
	if err := m.Steps(-1); err != nil {
		t.Fatalf("safe v99 to v98 rollback: %v", err)
	}
	assertPostgresMigrationVersion(t, db, 98, false)
	var dispatchCount int
	if err := db.QueryRow(`SELECT COALESCE((metadata->>'dispatch_count')::int, 0) FROM team_tasks WHERE id=$1`, ids.workflowTask).Scan(&dispatchCount); err != nil {
		t.Fatalf("read rolled-back dispatch_count metadata: %v", err)
	}
	if dispatchCount != 3 {
		t.Fatalf("rolled-back metadata.dispatch_count = %d, want 3", dispatchCount)
	}
	if err := m.Steps(1); err != nil {
		t.Fatalf("re-apply v99 after safe rollback: %v", err)
	}
	assertPostgresWorkflowEndpoint(t, db, ids)
	t.Log("safe PostgreSQL v99↔v98 round trip passed")

	// A lifecycle value that exists only in v99 must reject down migration. The
	// migration runner records the failed target as dirty; it must not coerce the
	// row into a predecessor status.
	if _, err := db.Exec(`UPDATE team_workflows SET status='needs_revision' WHERE id=$1`, ids.workflow); err != nil {
		t.Fatalf("set v99-only status: %v", err)
	}
	if err := m.Steps(-1); err == nil {
		t.Fatal("v99 down migration accepted needs_revision state")
	}
	assertPostgresMigrationVersion(t, db, 98, true)
	var workflowStatus string
	if err := db.QueryRow(`SELECT status FROM team_workflows WHERE id=$1`, ids.workflow).Scan(&workflowStatus); err != nil {
		t.Fatalf("read status after rejected down migration: %v", err)
	}
	if workflowStatus != "needs_revision" {
		t.Fatalf("rejected down migration changed status to %q", workflowStatus)
	}
	if err := m.Force(100); err != nil {
		t.Fatalf("restore migration cursor after expected status rejection: %v", err)
	}
	if _, err := db.Exec(`UPDATE team_workflows SET status='running' WHERE id=$1`, ids.workflow); err != nil {
		t.Fatalf("restore compatible status: %v", err)
	}
	t.Log("v99-only workflow status rejected rollback and marked migration dirty")

	// Non-default task enforcement state also has no v98 representation.
	if _, err := db.Exec(`UPDATE team_tasks SET blocker_reason='waiting for operator' WHERE id=$1`, ids.workflowTask); err != nil {
		t.Fatalf("set v99-only task state: %v", err)
	}
	if err := m.Steps(-1); err == nil {
		t.Fatal("v99 down migration accepted blocker state")
	}
	assertPostgresMigrationVersion(t, db, 98, true)
	var blockerReason string
	if err := db.QueryRow(`SELECT blocker_reason FROM team_tasks WHERE id=$1`, ids.workflowTask).Scan(&blockerReason); err != nil {
		t.Fatalf("read blocker state after rejected down migration: %v", err)
	}
	if blockerReason != "waiting for operator" {
		t.Fatalf("rejected down migration changed blocker reason to %q", blockerReason)
	}
	if err := m.Force(100); err != nil {
		t.Fatalf("restore migration cursor after expected task-state rejection: %v", err)
	}
	if _, err := db.Exec(`UPDATE team_tasks SET blocker_reason='' WHERE id=$1`, ids.workflowTask); err != nil {
		t.Fatalf("restore compatible blocker state: %v", err)
	}
	t.Log("v99-only task state rejected rollback without data loss")

	// Append-only classifier audits cannot be represented at v98. The rollback
	// must preserve the row rather than dropping the audit table.
	auditID := uuid.New()
	if _, err := db.Exec(`INSERT INTO team_work_classification_audits(id,tenant_id,ingress) VALUES($1,$2,'ws')`, auditID, ids.tenant); err != nil {
		t.Fatalf("seed classifier audit before rollback rejection: %v", err)
	}
	if err := m.Steps(-1); err == nil {
		t.Fatal("v99 down migration accepted classifier audit state")
	}
	assertPostgresMigrationVersion(t, db, 98, true)
	var auditCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM team_work_classification_audits WHERE id=$1`, auditID).Scan(&auditCount); err != nil {
		t.Fatalf("read audit after rejected down migration: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("rejected down migration retained %d audit rows, want 1", auditCount)
	}
	if err := m.Force(100); err != nil {
		t.Fatalf("restore migration cursor after expected audit rejection: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM team_work_classification_audits WHERE id=$1`, auditID); err != nil {
		t.Fatalf("remove rollback audit fixture: %v", err)
	}
	t.Log("classifier audit state rejected rollback without data loss")

	// Revision 2 may reuse a step ID in v99. Restoring the predecessor index must
	// reject that data rather than deleting or merging either task.
	revisionTwoTask := uuid.New()
	if _, err := db.Exec(`INSERT INTO team_tasks(
		id,team_id,subject,status,owner_agent_id,metadata,task_type,
		workflow_id,workflow_step_id,workflow_kind,workflow_terminal,
		plan_revision,notification_policy,tenant_id
	) VALUES($1,$2,'Revision 2','pending',NULL,'{}','general',$3,'step-1','work',FALSE,2,'workflow_internal',$4)`,
		revisionTwoTask, ids.team, ids.workflow, ids.tenant); err != nil {
		t.Fatalf("seed revision-2 task: %v", err)
	}
	if err := m.Steps(-1); err == nil {
		t.Fatal("v99 down migration accepted duplicate workflow step across revisions")
	}
	assertPostgresMigrationVersion(t, db, 98, true)
	var revisionTaskCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM team_tasks WHERE id IN ($1,$2)`, ids.workflowTask, revisionTwoTask).Scan(&revisionTaskCount); err != nil {
		t.Fatalf("count tasks after rejected revision rollback: %v", err)
	}
	if revisionTaskCount != 2 {
		t.Fatalf("rejected revision rollback retained %d tasks, want 2", revisionTaskCount)
	}
	if err := m.Force(100); err != nil {
		t.Fatalf("restore migration cursor after expected revision rejection: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM team_tasks WHERE id=$1`, revisionTwoTask); err != nil {
		t.Fatalf("remove revision-2 test task: %v", err)
	}
	t.Log("revision-only uniqueness state rejected rollback without data loss")

	if err := m.Migrate(95); err != nil {
		t.Fatalf("restore isolated schema to v95: %v", err)
	}
	assertPostgresMigrationVersion(t, db, 95, false)
	t.Log("restored isolated PostgreSQL schema to v95 before cleanup")
}

func TestPostgresWorkflowMigrationRejectsDuplicateActiveOwners(t *testing.T) {
	dsn := postgresWorkflowMigrationTestDSN(t)
	schema := "phase9_owner_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	scopedDSN := postgresDSNWithSearchPath(t, dsn, schema)

	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open disposable PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	if err := adminDB.Ping(); err != nil {
		t.Skipf("disposable PostgreSQL is not reachable: %v", err)
	}
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminDB.Exec("CREATE SCHEMA " + quotedSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	t.Cleanup(func() { _, _ = adminDB.Exec("DROP SCHEMA IF EXISTS " + quotedSchema + " CASCADE") })

	db, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		t.Fatalf("open isolated migration schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	m, err := migrate.New("file://../../migrations", scopedDSN)
	if err != nil {
		t.Fatalf("create migration runner: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })

	if err := m.Migrate(97); err != nil {
		t.Fatalf("migrate isolated schema to v97: %v", err)
	}
	ids := seedPostgresWorkflowV97(t, db)
	if _, err := db.Exec(`INSERT INTO team_tasks(
		id,team_id,subject,status,owner_agent_id,metadata,task_type,
		workflow_id,workflow_step_id,workflow_kind,workflow_terminal,tenant_id
	) VALUES($1,$2,'Duplicate active owner','dispatching',$3,'{}','general',$4,'step-2','work',FALSE,$5)`,
		uuid.New(), ids.team, ids.worker, ids.workflow, ids.tenant); err != nil {
		t.Fatalf("seed duplicate active owner: %v", err)
	}
	if err := m.Migrate(99); err == nil {
		t.Fatal("v99 migration silently accepted duplicate active workflow owners")
	}
	assertPostgresMigrationVersion(t, db, 99, true)
	var taskCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM team_tasks WHERE workflow_id=$1 AND owner_agent_id=$2 AND status IN ('dispatching','in_progress')`, ids.workflow, ids.worker).Scan(&taskCount); err != nil {
		t.Fatalf("count duplicate active-owner tasks: %v", err)
	}
	if taskCount != 2 {
		t.Fatalf("rejected owner migration retained %d active tasks, want 2", taskCount)
	}
	t.Log("duplicate active owners aborted PostgreSQL v99 migration without choosing a winner")
}

// TestPostgresWorkflowMigrationLiveEquivalentV98ToV99Idempotent reproduces the
// exact production upgrade the renumber must survive. The live VPS ran the
// PRE-renumber Team Work chain (base 96 → notification 97 → enforcement 98), so
// its schema_migrations cursor sits at 98 WITH every enforcement object already
// present. After this branch renumbers enforcement to 99, `migrate up` on that
// VPS runs ONLY 000100 (the one file whose version exceeds 98) against a database
// that already carries all of 99's columns, constraints, indexes and the audit
// table. If 000099 is not fully idempotent, that run fails with "column/constraint
// already exists", flips schema_migrations.dirty = true, and wedges production.
//
// The test also covers the second live hazard: the VPS branched before upstream
// added subagent_tasks.root_agent_id (upstream 000096), so it is permanently
// skipped on the VPS (96 < 98). 000100's convergence block must re-add it.
func TestPostgresWorkflowMigrationLiveEquivalentV98ToV99Idempotent(t *testing.T) {
	dsn := postgresWorkflowMigrationTestDSN(t)
	schema := "phase9_live98_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	scopedDSN := postgresDSNWithSearchPath(t, dsn, schema)

	adminDB, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open disposable PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = adminDB.Close() })
	if err := adminDB.Ping(); err != nil {
		t.Skipf("disposable PostgreSQL is not reachable: %v", err)
	}
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := adminDB.Exec("CREATE SCHEMA " + quotedSchema); err != nil {
		t.Fatalf("create isolated migration schema: %v", err)
	}
	t.Cleanup(func() { _, _ = adminDB.Exec("DROP SCHEMA IF EXISTS " + quotedSchema + " CASCADE") })

	db, err := sql.Open("pgx", scopedDSN)
	if err != nil {
		t.Fatalf("open isolated migration schema: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	m, err := migrate.New("file://../../migrations", scopedDSN)
	if err != nil {
		t.Fatalf("create migration runner: %v", err)
	}
	t.Cleanup(func() { _, _ = m.Close() })

	// Build the full endpoint via the real chain, seeding workflow state that must
	// survive the simulated re-run.
	if err := m.Migrate(95); err != nil {
		t.Fatalf("migrate isolated schema to v95: %v", err)
	}
	if err := m.Steps(1); err != nil { // v96 upstream subagent scope
		t.Fatalf("apply v96: %v", err)
	}
	if err := m.Steps(1); err != nil { // v97 workflows base
		t.Fatalf("apply v97: %v", err)
	}
	ids := seedPostgresWorkflowV97(t, db)
	if err := m.Steps(1); err != nil { // v98 notification policy
		t.Fatalf("apply v98: %v", err)
	}
	if err := m.Steps(1); err != nil { // v99 enforcement
		t.Fatalf("apply v99: %v", err)
	}
	assertPostgresWorkflowEndpoint(t, db, ids)

	// --- Scenario A: VPS cursor at 98, all enforcement objects already present. ---
	if err := m.Force(99); err != nil {
		t.Fatalf("simulate VPS cursor at v98: %v", err)
	}
	if err := m.Migrate(RequiredSchemaVersion); err != nil {
		t.Fatalf("live-equivalent 99→100 re-apply failed (000100 not idempotent): %v", err)
	}
	assertPostgresMigrationVersion(t, db, RequiredSchemaVersion, false)
	assertPostgresWorkflowEndpoint(t, db, ids)
	t.Log("live-equivalent 98→99 re-applied enforcement idempotently without dirty")

	// --- Scenario B: VPS also missed upstream subagent root-agent scope. ---
	if err := m.Force(99); err != nil {
		t.Fatalf("reset cursor to v98 for scope-gap scenario: %v", err)
	}
	// Dropping the column cascades to its FK + indexes, exactly the state of a VPS
	// that never ran upstream 000096.
	if _, err := db.Exec(`ALTER TABLE subagent_tasks DROP COLUMN IF EXISTS root_agent_id CASCADE`); err != nil {
		t.Fatalf("drop subagent scope column to mimic skipped upstream 000096: %v", err)
	}
	if hasSubagentRootAgentColumn(t, db) {
		t.Fatal("precondition failed: root_agent_id still present after drop")
	}
	if err := m.Migrate(RequiredSchemaVersion); err != nil {
		t.Fatalf("98→99 convergence of skipped subagent scope failed: %v", err)
	}
	assertPostgresMigrationVersion(t, db, RequiredSchemaVersion, false)
	if !hasSubagentRootAgentColumn(t, db) {
		t.Fatal("000099 convergence block did not re-add subagent_tasks.root_agent_id")
	}
	for _, index := range []string{
		"idx_subagent_tasks_root_status",
		"idx_subagent_tasks_root_session",
		"idx_subagent_tasks_root_archive",
	} {
		var def *string
		if err := db.QueryRow(`SELECT pg_get_indexdef(to_regclass(format('%I.%I', current_schema(), $1::text)))`, index).Scan(&def); err != nil {
			t.Fatalf("inspect %s: %v", index, err)
		}
		if def == nil {
			t.Fatalf("convergence did not recreate index %s", index)
		}
	}
	assertPostgresWorkflowEndpoint(t, db, ids)
	t.Log("98→99 converged the skipped upstream subagent scope on the live-equivalent VPS")
}

func hasSubagentRootAgentColumn(t *testing.T, db *sql.DB) bool {
	t.Helper()
	var exists bool
	// Scope by current_schema() (the test's isolated schema is first in the
	// connection's search_path). Without this, the query sees every
	// subagent_tasks table in the database — including the one the separately
	// migrated public schema carries — and reports the column present even after
	// it is dropped in the test schema.
	if err := db.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name='subagent_tasks' AND column_name='root_agent_id')`,
	).Scan(&exists); err != nil {
		t.Fatalf("inspect subagent_tasks.root_agent_id: %v", err)
	}
	return exists
}

type postgresWorkflowIDs struct {
	tenant, lead, worker, team, workflow, workflowTask, genericTask uuid.UUID
}

func seedPostgresWorkflowV97(t *testing.T, db *sql.DB) postgresWorkflowIDs {
	t.Helper()
	ids := postgresWorkflowIDs{
		tenant: uuid.New(), lead: uuid.New(), worker: uuid.New(), team: uuid.New(),
		workflow: uuid.New(), workflowTask: uuid.New(), genericTask: uuid.New(),
	}
	if _, err := db.Exec(`INSERT INTO tenants(id,name,slug,status,settings) VALUES($1,'Tenant',$2,'active','{}')`, ids.tenant, "phase9-"+ids.tenant.String()); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	for id, key := range map[uuid.UUID]string{ids.lead: "lead", ids.worker: "worker"} {
		if _, err := db.Exec(`INSERT INTO agents(id,agent_key,display_name,owner_id,provider,model,agent_type,status,tenant_id)
			VALUES($1,$2,$2,'owner','openai','test','predefined','active',$3)`, id, key+"-"+id.String(), ids.tenant); err != nil {
			t.Fatalf("seed agent: %v", err)
		}
	}
	if _, err := db.Exec(`INSERT INTO agent_teams(id,name,lead_agent_id,status,settings,created_by,tenant_id)
		VALUES($1,'Team',$2,'active','{}','owner',$3)`, ids.team, ids.lead, ids.tenant); err != nil {
		t.Fatalf("seed team: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO team_workflows(
		id,team_id,tenant_id,status,canonical_plan,schema_version,plan_hash,
		coordinator_agent_id,coordinator_agent_key,origin_agent_id,origin_agent_key,
		origin_run_id,origin_session_key,origin_channel,origin_chat_id,
		origin_peer_kind,origin_local_key,origin_user_id,origin_sender_id,origin_role,
		origin_routing,auto_expand,expansion_token,expansion_lease_until,
		finalize_token,finalize_lease_until,finalize_claimed_at,failure_settle_deadline,
		failure_summary,result_summary,delivery_status,delivery_token,delivery_lease_until
	) VALUES(
		$1,$2,$3,'running','{"steps":["preserve"]}',7,$4,
		$5,'lead',$5,'lead','run-v97','session-v97','ws','chat-v97',
		'group','local-v97','user-v97','sender-v97','member',
		'{"thread":"keep"}',TRUE,$6,'2030-01-01T00:00:00Z',
		$7,'2030-01-02T00:00:00Z','2029-12-01T00:00:00Z','2030-01-03T00:00:00Z',
		'failure-keep','result-keep','enqueuing',$8,'2030-01-04T00:00:00Z'
	)`, ids.workflow, ids.team, ids.tenant, strings.Repeat("a", 64), ids.lead, uuid.New(), uuid.New(), uuid.New()); err != nil {
		t.Fatalf("seed workflow: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO team_tasks(
		id,team_id,subject,description,status,owner_agent_id,metadata,task_type,
		workflow_id,workflow_step_id,workflow_kind,workflow_terminal,
		dispatch_token,dispatch_lease_until,tenant_id
	) VALUES($1,$2,'Work step','must survive','in_progress',$3,
		'{"dispatch_count":3,"keep":"yes"}','general',$4,'step-1','work',TRUE,$5,'2030-02-01T00:00:00Z',$6)`,
		ids.workflowTask, ids.team, ids.worker, ids.workflow, uuid.New(), ids.tenant); err != nil {
		t.Fatalf("seed workflow task: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO team_tasks(id,team_id,subject,status,owner_agent_id,metadata,task_type,tenant_id)
		VALUES($1,$2,'Generic task','pending',$3,'{"keep":"generic"}','general',$4)`, ids.genericTask, ids.team, ids.worker, ids.tenant); err != nil {
		t.Fatalf("seed generic task: %v", err)
	}
	if _, err := db.Exec(`UPDATE team_workflows SET terminal_task_id=$1 WHERE id=$2`, ids.workflowTask, ids.workflow); err != nil {
		t.Fatalf("link terminal task: %v", err)
	}
	return ids
}

func assertPostgresWorkflowEndpoint(t *testing.T, db *sql.DB, ids postgresWorkflowIDs) {
	t.Helper()
	assertPostgresMigrationVersion(t, db, RequiredSchemaVersion, false)
	var (
		status, canonicalPlan, routing, failureSummary, resultSummary, deliveryStatus string
		planRevision, expansionAttempts, deliveryAttempts                             int
	)
	if err := db.QueryRow(`SELECT status,canonical_plan::text,origin_routing::text,
		failure_summary,result_summary,delivery_status,
		plan_revision,expansion_attempt_count,delivery_attempt_count
		FROM team_workflows WHERE id=$1`, ids.workflow).Scan(
		&status, &canonicalPlan, &routing, &failureSummary, &resultSummary, &deliveryStatus,
		&planRevision, &expansionAttempts, &deliveryAttempts,
	); err != nil {
		t.Fatalf("read upgraded workflow: %v", err)
	}
	if status != "running" || failureSummary != "failure-keep" || resultSummary != "result-keep" || deliveryStatus != "enqueuing" {
		t.Fatalf("upgraded workflow changed state: status=%q failure=%q result=%q delivery=%q", status, failureSummary, resultSummary, deliveryStatus)
	}
	if !strings.Contains(canonicalPlan, "preserve") || !strings.Contains(routing, "keep") {
		t.Fatalf("upgraded workflow changed JSON: plan=%q routing=%q", canonicalPlan, routing)
	}
	if planRevision != 1 || expansionAttempts != 0 || deliveryAttempts != 0 {
		t.Fatalf("workflow defaults = revision %d expansion %d delivery %d", planRevision, expansionAttempts, deliveryAttempts)
	}

	var notificationPolicy, metadata string
	var dispatchCount, taskRevision int
	if err := db.QueryRow(`SELECT notification_policy,metadata::text,dispatch_count,plan_revision
		FROM team_tasks WHERE id=$1`, ids.workflowTask).Scan(&notificationPolicy, &metadata, &dispatchCount, &taskRevision); err != nil {
		t.Fatalf("read upgraded workflow task: %v", err)
	}
	if notificationPolicy != "workflow_internal" || dispatchCount != 3 || taskRevision != 1 || strings.Contains(metadata, "dispatch_count") || !strings.Contains(metadata, "keep") {
		t.Fatalf("workflow task backfill = policy %q metadata %q count %d revision %d", notificationPolicy, metadata, dispatchCount, taskRevision)
	}

	for index, fragments := range map[string][]string{
		"idx_team_tasks_workflow_step": {"tenant_id", "workflow_id", "plan_revision", "workflow_step_id"},
		"idx_team_tasks_active_owner":  {"tenant_id", "owner_agent_id", "workflow_kind", "work", "dispatching", "in_progress"},
		"idx_twc_audits_tenant_time":   {"tenant_id", "created_at DESC"},
	} {
		var indexDef string
		if err := db.QueryRow(`SELECT pg_get_indexdef(to_regclass(format('%I.%I', current_schema(), $1::text)))`, index).Scan(&indexDef); err != nil {
			t.Fatalf("inspect index %s: %v", index, err)
		}
		for _, fragment := range fragments {
			if !strings.Contains(indexDef, fragment) {
				t.Fatalf("endpoint index %s missing %q: %s", index, fragment, indexDef)
			}
		}
	}
	var auditTableExists bool
	if err := db.QueryRow(`SELECT to_regclass(format('%I.%I', current_schema(), 'team_work_classification_audits')) IS NOT NULL`).Scan(&auditTableExists); err != nil {
		t.Fatalf("inspect classifier audit table: %v", err)
	}
	if !auditTableExists {
		t.Fatal("endpoint missing team_work_classification_audits")
	}
	assertPostgresWorkflowConstraintAndAuditFKs(t, db, ids)
}

func assertPostgresWorkflowConstraintAndAuditFKs(t *testing.T, db *sql.DB, ids postgresWorkflowIDs) {
	t.Helper()

	for _, tc := range []struct {
		name  string
		query string
		args  []any
	}{
		{name: "workflow status", query: `UPDATE team_workflows SET status='invalid' WHERE id=$1`, args: []any{ids.workflow}},
		{name: "workflow delivery status", query: `UPDATE team_workflows SET delivery_status='invalid' WHERE id=$1`, args: []any{ids.workflow}},
		{name: "task escalation status", query: `UPDATE team_tasks SET escalation_status='invalid' WHERE id=$1`, args: []any{ids.workflowTask}},
		{name: "task notification policy", query: `UPDATE team_tasks SET notification_policy='invalid' WHERE id=$1`, args: []any{ids.workflowTask}},
		{name: "audit ingress", query: `INSERT INTO team_work_classification_audits(id,tenant_id,ingress) VALUES($1,$2,'invalid')`, args: []any{uuid.New(), ids.tenant}},
		{name: "audit requested mode", query: `INSERT INTO team_work_classification_audits(id,tenant_id,ingress,requested_mode) VALUES($1,$2,'ws','invalid')`, args: []any{uuid.New(), ids.tenant}},
		{name: "audit effective mode", query: `INSERT INTO team_work_classification_audits(id,tenant_id,ingress,effective_mode) VALUES($1,$2,'ws','invalid')`, args: []any{uuid.New(), ids.tenant}},
	} {
		if _, err := db.Exec(tc.query, tc.args...); err == nil {
			t.Fatalf("endpoint %s constraint accepted invalid value", tc.name)
		}
	}

	auditID := uuid.New()
	if _, err := db.Exec(`INSERT INTO team_work_classification_audits(id,tenant_id,ingress,requested_mode,effective_mode) VALUES($1,$2,'ws','multi_role','multi_role')`, auditID, ids.tenant); err != nil {
		t.Fatalf("insert valid classifier audit: %v", err)
	}
	if _, err := db.Exec(`UPDATE team_workflows SET classification_audit_id=$1 WHERE id=$2`, auditID, ids.workflow); err != nil {
		t.Fatalf("link classifier audit: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM team_work_classification_audits WHERE id=$1`, auditID); err != nil {
		t.Fatalf("delete linked classifier audit: %v", err)
	}
	var linkedAuditID *uuid.UUID
	if err := db.QueryRow(`SELECT classification_audit_id FROM team_workflows WHERE id=$1`, ids.workflow).Scan(&linkedAuditID); err != nil {
		t.Fatalf("read classifier audit FK after delete: %v", err)
	}
	if linkedAuditID != nil {
		t.Fatalf("classification_audit_id = %s after audit delete, want NULL", *linkedAuditID)
	}
}

func assertPostgresTaskPolicy(t *testing.T, db *sql.DB, taskID uuid.UUID, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(`SELECT notification_policy FROM team_tasks WHERE id=$1`, taskID).Scan(&got); err != nil {
		t.Fatalf("read task notification policy: %v", err)
	}
	if got != want {
		t.Fatalf("task %s policy = %q, want %q", taskID, got, want)
	}
}

func assertPostgresMigrationVersion(t *testing.T, db *sql.DB, want uint, wantDirty bool) {
	t.Helper()
	var version uint
	var dirty bool
	if err := db.QueryRow(`SELECT version,dirty FROM schema_migrations`).Scan(&version, &dirty); err != nil {
		t.Fatalf("read schema_migrations: %v", err)
	}
	if version != want || dirty != wantDirty {
		t.Fatalf("schema_migrations = (%d,%v), want (%d,%v)", version, dirty, want, wantDirty)
	}
}

func postgresWorkflowMigrationTestDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; PostgreSQL workflow migration test did not execute")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", err)
	}
	host := strings.Trim(config.Host, "[]")
	if config.Database != "goclaw_test" || config.Port != 55433 || !(host == "127.0.0.1" || host == "localhost" || net.ParseIP(host).IsLoopback()) {
		t.Fatalf("refusing PostgreSQL migration test outside disposable local goclaw_test: host=%q port=%d database=%q", config.Host, config.Port, config.Database)
	}
	return dsn
}

func postgresDSNWithSearchPath(t *testing.T, dsn, schema string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse PostgreSQL DSN URL: %v", err)
	}
	q := u.Query()
	q.Set("search_path", fmt.Sprintf("%s,public", schema))
	u.RawQuery = q.Encode()
	return u.String()
}

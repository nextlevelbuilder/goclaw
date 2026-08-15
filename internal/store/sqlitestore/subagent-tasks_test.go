//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func createSQLiteSubagentTask(
	t *testing.T,
	taskStore *SQLiteSubagentTaskStore,
	ctx context.Context,
	rootAgentID uuid.UUID,
	rootAgentKey, sessionKey, status string,
) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	task := &store.SubagentTaskData{
		RootAgentID:    rootAgentID,
		ParentAgentKey: rootAgentKey,
		SessionKey:     &sessionKey,
		Subject:        "store test",
		Description:    "verify scoped persistence",
		Status:         status,
		Depth:          1,
		Metadata:       map[string]any{},
	}
	task.ID = id
	if err := taskStore.Create(ctx, task); err != nil {
		t.Fatalf("Create(%s): %v", id, err)
	}
	return id
}

func TestSQLiteSubagentTaskStoreRequiresTenantAndRootScope(t *testing.T) {
	db := newHookTestDB(t)
	tenantA, rootAID := seedHookTenantAgent(t, db)
	tenantB, tenantBRootID := seedHookTenantAgent(t, db)
	ctxA := sqliteTenantCtx(tenantA)
	ctxB := sqliteTenantCtx(tenantB)
	taskStore := NewSQLiteSubagentTaskStore(db)

	const (
		rootA     = "root-a"
		rootB     = "root-b"
		sessionID = "shared-session"
	)
	rootBID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES (?,?,?,'predefined','active','test','test-model','owner')`,
		rootBID, tenantA, rootB,
	); err != nil {
		t.Fatalf("seed second root agent: %v", err)
	}
	taskA := createSQLiteSubagentTask(t, taskStore, ctxA, rootAID, rootA, sessionID, "queued")
	taskB := createSQLiteSubagentTask(t, taskStore, ctxA, rootBID, rootB, sessionID, "queued")
	delegationTask := createSQLiteSubagentTask(t, taskStore, ctxA, rootAID, rootA, sessionID, "queued")
	if err := taskStore.UpdateMetadata(ctxA, rootAID, delegationTask, map[string]any{
		"completion_kind": "delegate",
	}); err != nil {
		t.Fatalf("mark delegation completion: %v", err)
	}
	_ = createSQLiteSubagentTask(t, taskStore, ctxB, tenantBRootID, rootA, sessionID, "queued")
	crossTenantTask := &store.SubagentTaskData{
		BaseModel:      store.BaseModel{ID: uuid.Must(uuid.NewV7())},
		RootAgentID:    tenantBRootID,
		ParentAgentKey: rootA,
		Subject:        "cross tenant",
		Description:    "must be rejected",
		Status:         "queued",
		Depth:          1,
	}
	if err := taskStore.Create(ctxA, crossTenantTask); err == nil {
		t.Fatal("Create accepted root agent from another tenant")
	}

	got, err := taskStore.Get(ctxA, rootAID, taskA)
	if err != nil {
		t.Fatalf("Get owning scope: %v", err)
	}
	if got == nil || got.ID != taskA {
		t.Fatalf("Get owning scope = %#v, want task %s", got, taskA)
	}

	got, err = taskStore.Get(ctxA, rootBID, taskA)
	if err != nil {
		t.Fatalf("Get cross-root: %v", err)
	}
	if got != nil {
		t.Fatalf("Get cross-root = %#v, want nil", got)
	}

	got, err = taskStore.Get(ctxB, rootAID, taskA)
	if err != nil {
		t.Fatalf("Get cross-tenant: %v", err)
	}
	if got != nil {
		t.Fatalf("Get cross-tenant = %#v, want nil", got)
	}

	if err := taskStore.UpdateStatus(ctxA, rootBID, taskA, "completed", nil, 3, 10, 20); !errors.Is(err, store.ErrSubagentTaskNotFound) {
		t.Fatalf("UpdateStatus cross-root error = %v, want scoped not found", err)
	}
	got, err = taskStore.Get(ctxA, rootAID, taskA)
	if err != nil {
		t.Fatalf("Get after cross-root status update: %v", err)
	}
	if got.Status != "queued" || got.CompletedAt != nil {
		t.Fatalf("cross-root status update changed task: status=%q completed_at=%v", got.Status, got.CompletedAt)
	}

	if err := taskStore.UpdateMetadata(ctxA, rootBID, taskA, map[string]any{"denied": true}); !errors.Is(err, store.ErrSubagentTaskNotFound) {
		t.Fatalf("UpdateMetadata cross-root error = %v, want scoped not found", err)
	}
	got, err = taskStore.Get(ctxA, rootAID, taskA)
	if err != nil {
		t.Fatalf("Get after cross-root metadata update: %v", err)
	}
	if _, exists := got.Metadata["denied"]; exists {
		t.Fatalf("cross-root metadata update changed task: %#v", got.Metadata)
	}
	if err := taskStore.UpdateMetadata(ctxA, rootAID, taskA, map[string]any{
		"announcement_status": "undelivered",
		"delivered":           false,
	}); err != nil {
		t.Fatalf("UpdateMetadata owning scope: %v", err)
	}
	got, err = taskStore.Get(ctxA, rootAID, taskA)
	if err != nil {
		t.Fatalf("Get after owning metadata update: %v", err)
	}
	if got.Metadata["announcement_status"] != "undelivered" || got.Metadata["delivered"] != false {
		t.Fatalf("metadata JSON types were not preserved: %#v", got.Metadata)
	}

	parentTasksA, err := taskStore.ListByParent(ctxA, rootAID, "")
	if err != nil {
		t.Fatalf("ListByParent root A: %v", err)
	}
	if len(parentTasksA) != 1 || parentTasksA[0].ID != taskA {
		t.Fatalf("ListByParent root A = %#v, want only self-clone %s", parentTasksA, taskA)
	}
	parentTasksB, err := taskStore.ListByParent(ctxA, rootBID, "queued")
	if err != nil {
		t.Fatalf("ListByParent root B: %v", err)
	}
	if len(parentTasksB) != 1 || parentTasksB[0].ID != taskB {
		t.Fatalf("ListByParent root B = %#v, want only %s", parentTasksB, taskB)
	}

	tasksA, err := taskStore.ListBySession(ctxA, rootAID, sessionID)
	if err != nil {
		t.Fatalf("ListBySession root A: %v", err)
	}
	if len(tasksA) != 1 || tasksA[0].ID != taskA {
		t.Fatalf("ListBySession root A = %#v, want only self-clone %s", tasksA, taskA)
	}
	tasksB, err := taskStore.ListBySession(ctxA, rootBID, sessionID)
	if err != nil {
		t.Fatalf("ListBySession root B: %v", err)
	}
	if len(tasksB) != 1 || tasksB[0].ID != taskB {
		t.Fatalf("ListBySession root B = %#v, want only %s", tasksB, taskB)
	}

	if _, err := taskStore.Get(context.Background(), rootAID, taskA); err == nil {
		t.Fatal("Get without tenant context returned nil error")
	}
	if _, err := taskStore.Get(ctxA, uuid.Nil, taskA); !errors.Is(err, store.ErrSubagentRootAgentIDRequired) {
		t.Fatalf("Get empty root error = %v, want %v", err, store.ErrSubagentRootAgentIDRequired)
	}
}

func TestSQLiteSubagentTaskStoreRejectsRecreatedAgentWithSameKey(t *testing.T) {
	db := newHookTestDB(t)
	tenantID, oldRootAgentID := seedHookTenantAgent(t, db)
	ctx := sqliteTenantCtx(tenantID)
	taskStore := NewSQLiteSubagentTaskStore(db)

	const rootAgentKey = "recreated-root"
	if _, err := db.Exec(
		`UPDATE agents SET agent_key = ? WHERE id = ?`,
		rootAgentKey, oldRootAgentID,
	); err != nil {
		t.Fatalf("rename original root agent: %v", err)
	}
	taskID := createSQLiteSubagentTask(
		t, taskStore, ctx, oldRootAgentID, rootAgentKey, "recreated-root-session", "queued",
	)

	if _, err := db.Exec(`DELETE FROM agents WHERE id = ?`, oldRootAgentID); err != nil {
		t.Fatalf("delete original root agent: %v", err)
	}
	newRootAgentID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES (?,?,?,'predefined','active','test','test-model','owner')`,
		newRootAgentID, tenantID, rootAgentKey,
	); err != nil {
		t.Fatalf("recreate root agent: %v", err)
	}

	got, err := taskStore.Get(ctx, newRootAgentID, taskID)
	if err != nil {
		t.Fatalf("Get with recreated root agent: %v", err)
	}
	if got != nil {
		t.Fatalf("Get with recreated root agent returned old task %s", got.ID)
	}
	if err := taskStore.UpdateStatus(ctx, newRootAgentID, taskID, "completed", nil, 1, 2, 3); !errors.Is(err, store.ErrSubagentTaskNotFound) {
		t.Fatalf("UpdateStatus with recreated root agent error = %v, want scoped not found", err)
	}
	var status string
	var rootAgentID sql.NullString
	if err := db.QueryRow(
		`SELECT status, root_agent_id FROM subagent_tasks WHERE id = ?`, taskID,
	).Scan(&status, &rootAgentID); err != nil {
		t.Fatalf("read preserved old task: %v", err)
	}
	if status != "queued" || rootAgentID.Valid {
		t.Fatalf("old task after recreate: status=%q root_agent_id=%q, want queued/NULL", status, rootAgentID.String)
	}
}

func TestSQLiteSubagentTaskMigrationBackfillsOnlySafeOwners(t *testing.T) {
	db := newHookTestDB(t)
	tenantID, oldRootAgentID := seedHookTenantAgent(t, db)

	const rootAgentKey = "migration-recreated-root"
	if _, err := db.Exec(
		`UPDATE agents SET agent_key = ?, deleted_at = ? WHERE id = ?`,
		rootAgentKey, time.Now().UTC().Format(time.RFC3339Nano), oldRootAgentID,
	); err != nil {
		t.Fatalf("prepare historical root agent: %v", err)
	}
	newRootAgentID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES (?,?,?,'predefined','active','test','test-model','owner')`,
		newRootAgentID, tenantID, rootAgentKey,
	); err != nil {
		t.Fatalf("insert recreated root agent: %v", err)
	}

	const (
		safeRootKey      = "migration-safe-root"
		recreatedOnlyKey = "migration-hard-deleted-root"
	)
	safeRootAgentID := uuid.Must(uuid.NewV7())
	recreatedOnlyAgentID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(
		`INSERT INTO agents
		 (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id, created_at)
		 VALUES (?,?,?,'predefined','active','test','test-model','owner',?),
		        (?,?,?,'predefined','active','test','test-model','owner',?)`,
		safeRootAgentID, tenantID, safeRootKey, "2026-01-01T00:00:00Z",
		recreatedOnlyAgentID, tenantID, recreatedOnlyKey, "2026-03-01T00:00:00Z",
	); err != nil {
		t.Fatalf("insert temporal migration agents: %v", err)
	}

	metadataTaskID := uuid.Must(uuid.NewV7())
	ambiguousTaskID := uuid.Must(uuid.NewV7())
	safeTaskID := uuid.Must(uuid.NewV7())
	recreatedTaskID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(
		`INSERT INTO subagent_tasks
		 (id, tenant_id, parent_agent_key, subject, description, status, metadata, created_at)
		 VALUES (?,?,?,?,?,'queued',?,?),
		        (?,?,?,?,?,'queued','{}',?),
		        (?,?,?,?,?,'queued','{}',?),
		        (?,?,?,?,?,'queued','{}',?)`,
		metadataTaskID, tenantID, rootAgentKey, "metadata", "metadata owner",
		fmt.Sprintf(`{"root_agent_id":%q}`, oldRootAgentID.String()), "2026-04-01T00:00:00Z",
		ambiguousTaskID, tenantID, rootAgentKey, "ambiguous", "ambiguous owner", "2026-04-01T00:00:00Z",
		safeTaskID, tenantID, safeRootKey, "safe", "safe temporal owner", "2026-02-01T00:00:00Z",
		recreatedTaskID, tenantID, recreatedOnlyKey, "recreated", "older than current owner", "2026-02-01T00:00:00Z",
	); err != nil {
		t.Fatalf("insert legacy tasks: %v", err)
	}

	if _, err := db.Exec(`
		DROP TRIGGER trg_subagent_tasks_root_tenant_insert;
		DROP TRIGGER trg_subagent_tasks_root_tenant_update;
		DROP INDEX idx_subagent_tasks_root_archive;
		DROP INDEX idx_subagent_tasks_root_session;
		DROP INDEX idx_subagent_tasks_root_status;
		ALTER TABLE subagent_tasks DROP COLUMN root_agent_id;
		UPDATE schema_version SET version = 58;
	`); err != nil {
		t.Fatalf("prepare v58 schema: %v", err)
	}
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("migrate v58 to v59: %v", err)
	}

	var metadataOwner sql.NullString
	if err := db.QueryRow(
		`SELECT root_agent_id FROM subagent_tasks WHERE id = ?`, metadataTaskID,
	).Scan(&metadataOwner); err != nil {
		t.Fatalf("read metadata-owned task: %v", err)
	}
	if !metadataOwner.Valid || metadataOwner.String != oldRootAgentID.String() {
		t.Fatalf("metadata owner = %q, want %s", metadataOwner.String, oldRootAgentID)
	}
	var ambiguousOwner sql.NullString
	if err := db.QueryRow(
		`SELECT root_agent_id FROM subagent_tasks WHERE id = ?`, ambiguousTaskID,
	).Scan(&ambiguousOwner); err != nil {
		t.Fatalf("read ambiguous task: %v", err)
	}
	if ambiguousOwner.Valid {
		t.Fatalf("ambiguous key owner = %q, want NULL", ambiguousOwner.String)
	}
	var safeOwner sql.NullString
	if err := db.QueryRow(
		`SELECT root_agent_id FROM subagent_tasks WHERE id = ?`, safeTaskID,
	).Scan(&safeOwner); err != nil {
		t.Fatalf("read safely backfilled task: %v", err)
	}
	if !safeOwner.Valid || safeOwner.String != safeRootAgentID.String() {
		t.Fatalf("safe owner = %q, want %s", safeOwner.String, safeRootAgentID)
	}
	var recreatedOwner sql.NullString
	if err := db.QueryRow(
		`SELECT root_agent_id FROM subagent_tasks WHERE id = ?`, recreatedTaskID,
	).Scan(&recreatedOwner); err != nil {
		t.Fatalf("read recreated-owner task: %v", err)
	}
	if recreatedOwner.Valid {
		t.Fatalf("new same-key agent inherited older task: owner=%q", recreatedOwner.String)
	}
	var version int
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("read migrated schema version: %v", err)
	}
	if version != SchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, SchemaVersion)
	}
}

func TestSQLiteSubagentTaskStoreCompletedAtOnlyForTerminalStatus(t *testing.T) {
	db := newHookTestDB(t)
	tenantID, rootAgentID := seedHookTenantAgent(t, db)
	ctx := sqliteTenantCtx(tenantID)
	taskStore := NewSQLiteSubagentTaskStore(db)

	tests := []struct {
		status   string
		terminal bool
	}{
		{status: "new"},
		{status: "queued"},
		{status: "running"},
		{status: "waiting_child"},
		{status: "completed", terminal: true},
		{status: "failed", terminal: true},
		{status: "cancelled", terminal: true},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			rootAgentKey := "root-" + tt.status
			id := createSQLiteSubagentTask(t, taskStore, ctx, rootAgentID, rootAgentKey, "session-"+tt.status, "queued")
			if err := taskStore.UpdateStatus(ctx, rootAgentID, id, tt.status, nil, 0, 0, 0); err != nil {
				t.Fatalf("UpdateStatus(%q): %v", tt.status, err)
			}
			got, err := taskStore.Get(ctx, rootAgentID, id)
			if err != nil {
				t.Fatalf("Get(%q): %v", tt.status, err)
			}
			if (got.CompletedAt != nil) != tt.terminal {
				t.Fatalf("status %q completed_at = %v, terminal=%v", tt.status, got.CompletedAt, tt.terminal)
			}
		})
	}
}

func TestSQLiteSubagentTaskStoreRecoverInterrupted(t *testing.T) {
	db := newHookTestDB(t)
	tenantA, rootAID := seedHookTenantAgent(t, db)
	tenantB, rootBID := seedHookTenantAgent(t, db)
	ctxA := sqliteTenantCtx(tenantA)
	ctxB := sqliteTenantCtx(tenantB)
	taskStore := NewSQLiteSubagentTaskStore(db)

	queuedID := createSQLiteSubagentTask(t, taskStore, ctxA, rootAID, "root-a", "queued", "queued")
	if _, err := db.Exec(
		`UPDATE subagent_tasks SET completed_at = ? WHERE id = ?`,
		time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano),
		queuedID,
	); err != nil {
		t.Fatalf("seed malformed queued completed_at: %v", err)
	}
	runningID := createSQLiteSubagentTask(t, taskStore, ctxA, rootAID, "root-a", "running", "running")
	waitingID := createSQLiteSubagentTask(
		t, taskStore, ctxB, rootBID, "root-b", "waiting", "waiting_child",
	)
	if err := taskStore.UpdateMetadata(ctxB, rootBID, waitingID, map[string]any{
		"completion_kind": "delegate",
		"completion_media": []map[string]any{{
			"path":      ".delegations/completed-before-crash/report.pdf",
			"mime_type": "application/pdf",
		}},
	}); err != nil {
		t.Fatalf("record published artifact metadata: %v", err)
	}
	completedID := createSQLiteSubagentTask(t, taskStore, ctxB, rootBID, "root-b", "completed", "queued")
	completedResult := "already completed"
	if err := taskStore.UpdateStatus(
		ctxB, rootBID, completedID, "completed", &completedResult, 1, 2, 3,
	); err != nil {
		t.Fatalf("complete terminal task: %v", err)
	}

	if _, err := taskStore.RecoverInterrupted(ctxA); err == nil {
		t.Fatal("RecoverInterrupted accepted tenant-scoped context")
	}
	recoveryCtx := store.WithTenantID(context.Background(), store.MasterTenantID)
	recovered, err := taskStore.RecoverInterrupted(recoveryCtx)
	if err != nil {
		t.Fatalf("RecoverInterrupted: %v", err)
	}
	if recovered != 3 {
		t.Fatalf("RecoverInterrupted recovered %d tasks, want 3", recovered)
	}

	for _, item := range []struct {
		ctx  context.Context
		root uuid.UUID
		id   uuid.UUID
	}{
		{ctx: ctxA, root: rootAID, id: queuedID},
		{ctx: ctxA, root: rootAID, id: runningID},
		{ctx: ctxB, root: rootBID, id: waitingID},
	} {
		got, getErr := taskStore.Get(item.ctx, item.root, item.id)
		if getErr != nil {
			t.Fatalf("Get recovered task %s: %v", item.id, getErr)
		}
		if got.Status != "failed" || got.CompletedAt == nil || got.Result == nil ||
			!strings.Contains(*got.Result, "gateway stopped") {
			t.Fatalf("recovered task %s = %#v", item.id, got)
		}
		if item.id == waitingID {
			if got.Metadata["completion_kind"] != "delegate" ||
				got.Metadata["completion_media"] == nil {
				t.Fatalf("published artifact metadata was lost: %#v", got.Metadata)
			}
		}
	}

	completed, err := taskStore.Get(ctxB, rootBID, completedID)
	if err != nil {
		t.Fatalf("Get completed task: %v", err)
	}
	if completed.Status != "completed" || completed.Result == nil ||
		*completed.Result != completedResult {
		t.Fatalf("completed task changed during recovery: %#v", completed)
	}

	recovered, err = taskStore.RecoverInterrupted(recoveryCtx)
	if err != nil {
		t.Fatalf("RecoverInterrupted second pass: %v", err)
	}
	if recovered != 0 {
		t.Fatalf("RecoverInterrupted second pass recovered %d tasks, want 0", recovered)
	}
}

func TestSQLiteSubagentTaskStoreArchiveIsScopedAndBounded(t *testing.T) {
	db := newHookTestDB(t)
	tenantA, rootAID := seedHookTenantAgent(t, db)
	tenantB, tenantBRootID := seedHookTenantAgent(t, db)
	ctxA := sqliteTenantCtx(tenantA)
	ctxB := sqliteTenantCtx(tenantB)
	taskStore := NewSQLiteSubagentTaskStore(db)

	const (
		rootA = "root-a"
		rootB = "root-b"
	)
	rootBID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES (?,?,?,'predefined','active','test','test-model','owner')`,
		rootBID, tenantA, rootB,
	); err != nil {
		t.Fatalf("seed second root agent: %v", err)
	}
	var rootATasks []uuid.UUID
	for i := 0; i < 3; i++ {
		rootATasks = append(rootATasks, createSQLiteSubagentTask(
			t, taskStore, ctxA, rootAID, rootA, fmt.Sprintf("session-a-%d", i), "queued",
		))
	}
	rootBTask := createSQLiteSubagentTask(t, taskStore, ctxA, rootBID, rootB, "session-b", "queued")
	tenantBTask := createSQLiteSubagentTask(t, taskStore, ctxB, tenantBRootID, rootA, "session-other-tenant", "queued")
	queuedTask := createSQLiteSubagentTask(t, taskStore, ctxA, rootAID, rootA, "session-queued", "queued")

	for _, item := range []struct {
		ctx  context.Context
		root uuid.UUID
		id   uuid.UUID
	}{
		{ctx: ctxA, root: rootAID, id: rootATasks[0]},
		{ctx: ctxA, root: rootAID, id: rootATasks[1]},
		{ctx: ctxA, root: rootAID, id: rootATasks[2]},
		{ctx: ctxA, root: rootBID, id: rootBTask},
		{ctx: ctxB, root: tenantBRootID, id: tenantBTask},
	} {
		if err := taskStore.UpdateStatus(item.ctx, item.root, item.id, "completed", nil, 0, 0, 0); err != nil {
			t.Fatalf("UpdateStatus(%s): %v", item.id, err)
		}
	}

	oldTime := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339Nano)
	for _, id := range append(append([]uuid.UUID{}, rootATasks...), rootBTask, tenantBTask) {
		if _, err := db.Exec(`UPDATE subagent_tasks SET completed_at = ? WHERE id = ?`, oldTime, id); err != nil {
			t.Fatalf("backdate terminal task %s: %v", id, err)
		}
	}
	// A non-terminal row must remain unarchived even if malformed legacy data
	// happens to carry a completed_at value.
	if _, err := db.Exec(`UPDATE subagent_tasks SET completed_at = ? WHERE id = ?`, oldTime, queuedTask); err != nil {
		t.Fatalf("backdate queued task: %v", err)
	}

	archived, err := taskStore.Archive(ctxA, rootAID, time.Hour, 2)
	if err != nil {
		t.Fatalf("Archive first batch: %v", err)
	}
	if archived != 2 {
		t.Fatalf("Archive first batch affected %d rows, want 2", archived)
	}

	assertSQLiteArchivedCount(t, db, tenantA, rootAID, 2)
	assertSQLiteArchivedCount(t, db, tenantA, rootBID, 0)
	assertSQLiteArchivedCount(t, db, tenantB, tenantBRootID, 0)

	archived, err = taskStore.Archive(ctxA, rootAID, time.Hour, 2)
	if err != nil {
		t.Fatalf("Archive second batch: %v", err)
	}
	if archived != 1 {
		t.Fatalf("Archive second batch affected %d rows, want 1", archived)
	}
	assertSQLiteArchivedCount(t, db, tenantA, rootAID, 3)

	var queuedArchived sql.NullString
	if err := db.QueryRow(`SELECT archived_at FROM subagent_tasks WHERE id = ?`, queuedTask).Scan(&queuedArchived); err != nil {
		t.Fatalf("read queued archived_at: %v", err)
	}
	if queuedArchived.Valid {
		t.Fatalf("queued task archived_at = %q, want NULL", queuedArchived.String)
	}
}

func assertSQLiteArchivedCount(
	t *testing.T, db *sql.DB, tenantID, rootAgentID uuid.UUID, want int,
) {
	t.Helper()
	var got int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM subagent_tasks
		 WHERE tenant_id = ? AND root_agent_id = ? AND archived_at IS NOT NULL`,
		tenantID, rootAgentID,
	).Scan(&got); err != nil {
		t.Fatalf("count archived tasks for %s/%s: %v", tenantID, rootAgentID, err)
	}
	if got != want {
		t.Fatalf("archived tasks for %s/%s = %d, want %d", tenantID, rootAgentID, got, want)
	}
}

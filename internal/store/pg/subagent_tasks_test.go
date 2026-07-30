package pg

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

func createPGSubagentTask(
	t *testing.T,
	taskStore *PGSubagentTaskStore,
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

func TestPGSubagentTaskStoreRequiresTenantAndRootScope(t *testing.T) {
	db := hooksTestDB(t)
	tenantA, rootAID := seedTenantAndAgent(t, db)
	tenantB, tenantBRootID := seedTenantAndAgent(t, db)
	ctxA := tenantScopedCtx(tenantA)
	ctxB := tenantScopedCtx(tenantB)
	taskStore := NewPGSubagentTaskStore(db)

	const (
		rootA     = "root-a"
		rootB     = "root-b"
		sessionID = "shared-session"
	)
	rootBID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES ($1,$2,$3,'predefined','active','test','test-model','owner')`,
		rootBID, tenantA, rootB,
	); err != nil {
		t.Fatalf("seed second root agent: %v", err)
	}
	taskA := createPGSubagentTask(t, taskStore, ctxA, rootAID, rootA, sessionID, "queued")
	taskB := createPGSubagentTask(t, taskStore, ctxA, rootBID, rootB, sessionID, "queued")
	delegationTask := createPGSubagentTask(t, taskStore, ctxA, rootAID, rootA, sessionID, "queued")
	if err := taskStore.UpdateMetadata(ctxA, rootAID, delegationTask, map[string]any{
		"completion_kind": "delegate",
	}); err != nil {
		t.Fatalf("mark delegation completion: %v", err)
	}
	_ = createPGSubagentTask(t, taskStore, ctxB, tenantBRootID, rootA, sessionID, "queued")
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

func TestPGSubagentTaskStoreRejectsRecreatedAgentWithSameKey(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, oldRootAgentID := seedTenantAndAgent(t, db)
	ctx := tenantScopedCtx(tenantID)
	taskStore := NewPGSubagentTaskStore(db)

	const rootAgentKey = "recreated-root"
	if _, err := db.Exec(
		`UPDATE agents SET agent_key = $1 WHERE id = $2`,
		rootAgentKey, oldRootAgentID,
	); err != nil {
		t.Fatalf("rename original root agent: %v", err)
	}
	taskID := createPGSubagentTask(
		t, taskStore, ctx, oldRootAgentID, rootAgentKey, "recreated-root-session", "queued",
	)

	if _, err := db.Exec(`DELETE FROM agents WHERE id = $1`, oldRootAgentID); err != nil {
		t.Fatalf("delete original root agent: %v", err)
	}
	newRootAgentID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES ($1,$2,$3,'predefined','active','test','test-model','owner')`,
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
	var rootAgentID uuid.NullUUID
	if err := db.QueryRow(
		`SELECT status, root_agent_id FROM subagent_tasks WHERE id = $1`, taskID,
	).Scan(&status, &rootAgentID); err != nil {
		t.Fatalf("read preserved old task: %v", err)
	}
	if status != "queued" || rootAgentID.Valid {
		t.Fatalf("old task after recreate: status=%q root_agent_id=%s, want queued/NULL", status, rootAgentID.UUID)
	}
}

func TestPGSubagentTaskStoreCompletedAtOnlyForTerminalStatus(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, rootAgentID := seedTenantAndAgent(t, db)
	ctx := tenantScopedCtx(tenantID)
	taskStore := NewPGSubagentTaskStore(db)

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
			id := createPGSubagentTask(t, taskStore, ctx, rootAgentID, rootAgentKey, "session-"+tt.status, "queued")
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

func TestPGSubagentTaskStoreRecoverInterrupted(t *testing.T) {
	db := hooksTestDB(t)
	// Recovery is intentionally process-global. Reset its shared table so rows
	// left by other store tests cannot affect the exact recovery count.
	if _, err := db.Exec(`DELETE FROM subagent_tasks`); err != nil {
		t.Fatalf("reset subagent tasks before global recovery: %v", err)
	}
	tenantA, rootAID := seedTenantAndAgent(t, db)
	tenantB, rootBID := seedTenantAndAgent(t, db)
	ctxA := tenantScopedCtx(tenantA)
	ctxB := tenantScopedCtx(tenantB)
	taskStore := NewPGSubagentTaskStore(db)

	queuedID := createPGSubagentTask(t, taskStore, ctxA, rootAID, "root-a", "queued", "queued")
	if _, err := db.Exec(
		`UPDATE subagent_tasks SET completed_at = $1 WHERE id = $2`,
		time.Now().UTC().Add(-time.Hour),
		queuedID,
	); err != nil {
		t.Fatalf("seed malformed queued completed_at: %v", err)
	}
	runningID := createPGSubagentTask(t, taskStore, ctxA, rootAID, "root-a", "running", "running")
	waitingID := createPGSubagentTask(
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
	completedID := createPGSubagentTask(t, taskStore, ctxB, rootBID, "root-b", "completed", "queued")
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

func TestPGSubagentTaskStoreArchiveIsScopedAndBounded(t *testing.T) {
	db := hooksTestDB(t)
	tenantA, rootAID := seedTenantAndAgent(t, db)
	tenantB, tenantBRootID := seedTenantAndAgent(t, db)
	ctxA := tenantScopedCtx(tenantA)
	ctxB := tenantScopedCtx(tenantB)
	taskStore := NewPGSubagentTaskStore(db)

	const (
		rootA = "root-a"
		rootB = "root-b"
	)
	rootBID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES ($1,$2,$3,'predefined','active','test','test-model','owner')`,
		rootBID, tenantA, rootB,
	); err != nil {
		t.Fatalf("seed second root agent: %v", err)
	}
	var rootATasks []uuid.UUID
	for i := range 3 {
		rootATasks = append(rootATasks, createPGSubagentTask(
			t, taskStore, ctxA, rootAID, rootA, fmt.Sprintf("session-a-%d", i), "queued",
		))
	}
	rootBTask := createPGSubagentTask(t, taskStore, ctxA, rootBID, rootB, "session-b", "queued")
	tenantBTask := createPGSubagentTask(t, taskStore, ctxB, tenantBRootID, rootA, "session-other-tenant", "queued")
	queuedTask := createPGSubagentTask(t, taskStore, ctxA, rootAID, rootA, "session-queued", "queued")

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

	oldTime := time.Now().UTC().Add(-2 * time.Hour)
	for _, id := range append(append([]uuid.UUID{}, rootATasks...), rootBTask, tenantBTask) {
		if _, err := db.Exec(`UPDATE subagent_tasks SET completed_at = $1 WHERE id = $2`, oldTime, id); err != nil {
			t.Fatalf("backdate terminal task %s: %v", id, err)
		}
	}
	// A non-terminal row must remain unarchived even if malformed legacy data
	// happens to carry a completed_at value.
	if _, err := db.Exec(`UPDATE subagent_tasks SET completed_at = $1 WHERE id = $2`, oldTime, queuedTask); err != nil {
		t.Fatalf("backdate queued task: %v", err)
	}

	archived, err := taskStore.Archive(ctxA, rootAID, time.Hour, 2)
	if err != nil {
		t.Fatalf("Archive first batch: %v", err)
	}
	if archived != 2 {
		t.Fatalf("Archive first batch affected %d rows, want 2", archived)
	}

	assertPGArchivedCount(t, db, tenantA, rootAID, 2)
	assertPGArchivedCount(t, db, tenantA, rootBID, 0)
	assertPGArchivedCount(t, db, tenantB, tenantBRootID, 0)

	archived, err = taskStore.Archive(ctxA, rootAID, time.Hour, 2)
	if err != nil {
		t.Fatalf("Archive second batch: %v", err)
	}
	if archived != 1 {
		t.Fatalf("Archive second batch affected %d rows, want 1", archived)
	}
	assertPGArchivedCount(t, db, tenantA, rootAID, 3)

	var queuedArchived sql.NullTime
	if err := db.QueryRow(`SELECT archived_at FROM subagent_tasks WHERE id = $1`, queuedTask).Scan(&queuedArchived); err != nil {
		t.Fatalf("read queued archived_at: %v", err)
	}
	if queuedArchived.Valid {
		t.Fatalf("queued task archived_at = %s, want NULL", queuedArchived.Time)
	}
}

func assertPGArchivedCount(
	t *testing.T, db *sql.DB, tenantID, rootAgentID uuid.UUID, want int,
) {
	t.Helper()
	var got int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM subagent_tasks
		 WHERE tenant_id = $1 AND root_agent_id = $2 AND archived_at IS NOT NULL`,
		tenantID, rootAgentID,
	).Scan(&got); err != nil {
		t.Fatalf("count archived tasks for %s/%s: %v", tenantID, rootAgentID, err)
	}
	if got != want {
		t.Fatalf("archived tasks for %s/%s = %d, want %d", tenantID, rootAgentID, got, want)
	}
}

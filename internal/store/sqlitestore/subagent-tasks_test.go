//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func createSQLiteSubagentTask(
	t *testing.T,
	taskStore *SQLiteSubagentTaskStore,
	ctx context.Context,
	rootAgentKey, sessionKey, status string,
) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	task := &store.SubagentTaskData{
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
	tenantA, _ := seedHookTenantAgent(t, db)
	tenantB, _ := seedHookTenantAgent(t, db)
	ctxA := sqliteTenantCtx(tenantA)
	ctxB := sqliteTenantCtx(tenantB)
	taskStore := NewSQLiteSubagentTaskStore(db)

	const (
		rootA     = "root-a"
		rootB     = "root-b"
		sessionID = "shared-session"
	)
	taskA := createSQLiteSubagentTask(t, taskStore, ctxA, rootA, sessionID, "queued")
	taskB := createSQLiteSubagentTask(t, taskStore, ctxA, rootB, sessionID, "queued")
	_ = createSQLiteSubagentTask(t, taskStore, ctxB, rootA, sessionID, "queued")

	got, err := taskStore.Get(ctxA, rootA, taskA)
	if err != nil {
		t.Fatalf("Get owning scope: %v", err)
	}
	if got == nil || got.ID != taskA {
		t.Fatalf("Get owning scope = %#v, want task %s", got, taskA)
	}

	got, err = taskStore.Get(ctxA, rootB, taskA)
	if err != nil {
		t.Fatalf("Get cross-root: %v", err)
	}
	if got != nil {
		t.Fatalf("Get cross-root = %#v, want nil", got)
	}

	got, err = taskStore.Get(ctxB, rootA, taskA)
	if err != nil {
		t.Fatalf("Get cross-tenant: %v", err)
	}
	if got != nil {
		t.Fatalf("Get cross-tenant = %#v, want nil", got)
	}

	if err := taskStore.UpdateStatus(ctxA, rootB, taskA, "completed", nil, 3, 10, 20); err != nil {
		t.Fatalf("UpdateStatus cross-root: %v", err)
	}
	got, err = taskStore.Get(ctxA, rootA, taskA)
	if err != nil {
		t.Fatalf("Get after cross-root status update: %v", err)
	}
	if got.Status != "queued" || got.CompletedAt != nil {
		t.Fatalf("cross-root status update changed task: status=%q completed_at=%v", got.Status, got.CompletedAt)
	}

	if err := taskStore.UpdateMetadata(ctxA, rootB, taskA, map[string]any{"denied": true}); err != nil {
		t.Fatalf("UpdateMetadata cross-root: %v", err)
	}
	got, err = taskStore.Get(ctxA, rootA, taskA)
	if err != nil {
		t.Fatalf("Get after cross-root metadata update: %v", err)
	}
	if _, exists := got.Metadata["denied"]; exists {
		t.Fatalf("cross-root metadata update changed task: %#v", got.Metadata)
	}

	tasksA, err := taskStore.ListBySession(ctxA, rootA, sessionID)
	if err != nil {
		t.Fatalf("ListBySession root A: %v", err)
	}
	if len(tasksA) != 1 || tasksA[0].ID != taskA {
		t.Fatalf("ListBySession root A = %#v, want only %s", tasksA, taskA)
	}
	tasksB, err := taskStore.ListBySession(ctxA, rootB, sessionID)
	if err != nil {
		t.Fatalf("ListBySession root B: %v", err)
	}
	if len(tasksB) != 1 || tasksB[0].ID != taskB {
		t.Fatalf("ListBySession root B = %#v, want only %s", tasksB, taskB)
	}

	if _, err := taskStore.Get(context.Background(), rootA, taskA); err == nil {
		t.Fatal("Get without tenant context returned nil error")
	}
	if _, err := taskStore.Get(ctxA, "", taskA); !errors.Is(err, store.ErrSubagentRootAgentKeyRequired) {
		t.Fatalf("Get empty root error = %v, want %v", err, store.ErrSubagentRootAgentKeyRequired)
	}
}

func TestSQLiteSubagentTaskStoreCompletedAtOnlyForTerminalStatus(t *testing.T) {
	db := newHookTestDB(t)
	tenantID, _ := seedHookTenantAgent(t, db)
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
			id := createSQLiteSubagentTask(t, taskStore, ctx, rootAgentKey, "session-"+tt.status, "queued")
			if err := taskStore.UpdateStatus(ctx, rootAgentKey, id, tt.status, nil, 0, 0, 0); err != nil {
				t.Fatalf("UpdateStatus(%q): %v", tt.status, err)
			}
			got, err := taskStore.Get(ctx, rootAgentKey, id)
			if err != nil {
				t.Fatalf("Get(%q): %v", tt.status, err)
			}
			if (got.CompletedAt != nil) != tt.terminal {
				t.Fatalf("status %q completed_at = %v, terminal=%v", tt.status, got.CompletedAt, tt.terminal)
			}
		})
	}
}

func TestSQLiteSubagentTaskStoreArchiveIsScopedAndBounded(t *testing.T) {
	db := newHookTestDB(t)
	tenantA, _ := seedHookTenantAgent(t, db)
	tenantB, _ := seedHookTenantAgent(t, db)
	ctxA := sqliteTenantCtx(tenantA)
	ctxB := sqliteTenantCtx(tenantB)
	taskStore := NewSQLiteSubagentTaskStore(db)

	const (
		rootA = "root-a"
		rootB = "root-b"
	)
	var rootATasks []uuid.UUID
	for i := 0; i < 3; i++ {
		rootATasks = append(rootATasks, createSQLiteSubagentTask(
			t, taskStore, ctxA, rootA, fmt.Sprintf("session-a-%d", i), "queued",
		))
	}
	rootBTask := createSQLiteSubagentTask(t, taskStore, ctxA, rootB, "session-b", "queued")
	tenantBTask := createSQLiteSubagentTask(t, taskStore, ctxB, rootA, "session-other-tenant", "queued")
	queuedTask := createSQLiteSubagentTask(t, taskStore, ctxA, rootA, "session-queued", "queued")

	for _, item := range []struct {
		ctx  context.Context
		root string
		id   uuid.UUID
	}{
		{ctx: ctxA, root: rootA, id: rootATasks[0]},
		{ctx: ctxA, root: rootA, id: rootATasks[1]},
		{ctx: ctxA, root: rootA, id: rootATasks[2]},
		{ctx: ctxA, root: rootB, id: rootBTask},
		{ctx: ctxB, root: rootA, id: tenantBTask},
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

	archived, err := taskStore.Archive(ctxA, rootA, time.Hour, 2)
	if err != nil {
		t.Fatalf("Archive first batch: %v", err)
	}
	if archived != 2 {
		t.Fatalf("Archive first batch affected %d rows, want 2", archived)
	}

	assertSQLiteArchivedCount(t, db, tenantA, rootA, 2)
	assertSQLiteArchivedCount(t, db, tenantA, rootB, 0)
	assertSQLiteArchivedCount(t, db, tenantB, rootA, 0)

	archived, err = taskStore.Archive(ctxA, rootA, time.Hour, 2)
	if err != nil {
		t.Fatalf("Archive second batch: %v", err)
	}
	if archived != 1 {
		t.Fatalf("Archive second batch affected %d rows, want 1", archived)
	}
	assertSQLiteArchivedCount(t, db, tenantA, rootA, 3)

	var queuedArchived sql.NullString
	if err := db.QueryRow(`SELECT archived_at FROM subagent_tasks WHERE id = ?`, queuedTask).Scan(&queuedArchived); err != nil {
		t.Fatalf("read queued archived_at: %v", err)
	}
	if queuedArchived.Valid {
		t.Fatalf("queued task archived_at = %q, want NULL", queuedArchived.String)
	}
}

func assertSQLiteArchivedCount(
	t *testing.T, db *sql.DB, tenantID uuid.UUID, rootAgentKey string, want int,
) {
	t.Helper()
	var got int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM subagent_tasks
		 WHERE tenant_id = ? AND parent_agent_key = ? AND archived_at IS NOT NULL`,
		tenantID, rootAgentKey,
	).Scan(&got); err != nil {
		t.Fatalf("count archived tasks for %s/%s: %v", tenantID, rootAgentKey, err)
	}
	if got != want {
		t.Fatalf("archived tasks for %s/%s = %d, want %d", tenantID, rootAgentKey, got, want)
	}
}

//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteRunTimelineStoreAppendAndListBySeq(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	timeline := NewSQLiteRunTimelineStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	agentID := uuid.Must(uuid.NewV7())
	seedSQLiteRunTimelineAgent(t, db, store.MasterTenantID, agentID)

	items := []store.RunTimelineItem{
		{
			RunID:      "run-1",
			SessionKey: "agent:default:direct:user-1",
			AgentID:    &agentID,
			UserID:     "user-1",
			Channel:    "web",
			Seq:        2,
			ItemType:   store.RunTimelineItemTypeToolCall,
			Status:     store.RunTimelineStatusRunning,
			Title:      "read_file",
			Preview:    `{"path":"/tmp/a.txt"}`,
			ToolName:   "read_file",
			ToolCallID: "call-1",
			Metadata:   json.RawMessage(`{"safe":true}`),
		},
		{
			RunID:      "run-1",
			SessionKey: "agent:default:direct:user-1",
			UserID:     "user-1",
			Channel:    "web",
			Seq:        1,
			ItemType:   store.RunTimelineItemTypeRunStatus,
			Status:     store.RunTimelineStatusStarted,
			Title:      "Run started",
			Preview:    "hello",
		},
	}
	for i := range items {
		if err := timeline.AppendRunTimelineItem(ctx, &items[i]); err != nil {
			t.Fatalf("AppendRunTimelineItem(%d): %v", i, err)
		}
	}

	got, err := timeline.ListRunTimelineItems(ctx, store.RunTimelineListOpts{RunID: "run-1", Limit: 10})
	if err != nil {
		t.Fatalf("ListRunTimelineItems: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(got))
	}
	if got[0].Seq != 1 || got[1].Seq != 2 {
		t.Fatalf("seq order = [%d,%d], want [1,2]", got[0].Seq, got[1].Seq)
	}
	if got[1].Content != "" {
		t.Fatalf("Content persisted = %q, want empty preview-only archive", got[1].Content)
	}
	if got[1].AgentID == nil || *got[1].AgentID != agentID {
		t.Fatalf("AgentID = %v, want %s", got[1].AgentID, agentID)
	}
}

func TestSQLiteRunTimelineStoreTenantScope(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	timeline := NewSQLiteRunTimelineStore(db)
	tenantA := uuid.Must(uuid.NewV7())
	tenantB := uuid.Must(uuid.NewV7())
	seedSQLiteRunTimelineTenant(t, db, tenantA)
	seedSQLiteRunTimelineTenant(t, db, tenantB)
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	item := store.RunTimelineItem{
		RunID:      "run-shared",
		SessionKey: "agent:default:direct:user-1",
		Seq:        1,
		ItemType:   store.RunTimelineItemTypeAssistantMessage,
		Status:     store.RunTimelineStatusCompleted,
		Title:      "Assistant",
		Preview:    "Visible only to tenant A",
		Content:    `{"raw":"must not persist"}`,
	}
	if err := timeline.AppendRunTimelineItem(ctxA, &item); err != nil {
		t.Fatalf("AppendRunTimelineItem: %v", err)
	}

	gotA, err := timeline.ListRunTimelineItems(ctxA, store.RunTimelineListOpts{RunID: "run-shared"})
	if err != nil {
		t.Fatalf("List tenant A: %v", err)
	}
	if len(gotA) != 1 {
		t.Fatalf("tenant A len = %d, want 1", len(gotA))
	}
	if gotA[0].Content != "" {
		t.Fatalf("Content = %q, want empty", gotA[0].Content)
	}

	gotB, err := timeline.ListRunTimelineItems(ctxB, store.RunTimelineListOpts{RunID: "run-shared"})
	if err != nil {
		t.Fatalf("List tenant B: %v", err)
	}
	if len(gotB) != 0 {
		t.Fatalf("tenant B len = %d, want 0", len(gotB))
	}

	gotNoTenant, err := timeline.ListRunTimelineItems(context.Background(), store.RunTimelineListOpts{RunID: "run-shared"})
	if err != nil {
		t.Fatalf("List no tenant: %v", err)
	}
	if len(gotNoTenant) != 0 {
		t.Fatalf("no-tenant len = %d, want fail-closed 0", len(gotNoTenant))
	}
}

func TestSQLiteRunTimelineStoreRecoverInterruptedRuns(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	timeline := NewSQLiteRunTimelineStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	add := func(item store.RunTimelineItem) {
		t.Helper()
		item.SessionKey = "agent:default:direct:user-1"
		if err := timeline.AppendRunTimelineItem(ctx, &item); err != nil {
			t.Fatalf("AppendRunTimelineItem(%s/%d): %v", item.RunID, item.Seq, err)
		}
	}

	// run-interrupted: started + an in-flight tool.call, no terminal run.status.
	add(store.RunTimelineItem{RunID: "run-interrupted", Seq: 1, ItemType: store.RunTimelineItemTypeRunStatus, Status: store.RunTimelineStatusStarted, Title: "Run started"})
	add(store.RunTimelineItem{RunID: "run-interrupted", Seq: 2, ItemType: store.RunTimelineItemTypeToolCall, Status: store.RunTimelineStatusRunning, Title: "exec"})
	// run-done: started + completed terminal — must be left untouched.
	add(store.RunTimelineItem{RunID: "run-done", Seq: 1, ItemType: store.RunTimelineItemTypeRunStatus, Status: store.RunTimelineStatusStarted, Title: "Run started"})
	add(store.RunTimelineItem{RunID: "run-done", Seq: 2, ItemType: store.RunTimelineItemTypeRunStatus, Status: store.RunTimelineStatusCompleted, Title: "Run completed"})

	n, err := timeline.RecoverInterruptedRuns(context.Background())
	if err != nil {
		t.Fatalf("RecoverInterruptedRuns: %v", err)
	}
	if n != 1 {
		t.Fatalf("recovered = %d, want 1 (only run-interrupted)", n)
	}

	// run-interrupted gained a terminal failed run.status at seq 3.
	got, err := timeline.ListRunTimelineItems(ctx, store.RunTimelineListOpts{RunID: "run-interrupted", Limit: 10})
	if err != nil {
		t.Fatalf("List run-interrupted: %v", err)
	}
	var terminal *store.RunTimelineItem
	for i := range got {
		if got[i].ItemType == store.RunTimelineItemTypeRunStatus && got[i].Status == store.RunTimelineStatusFailed {
			terminal = &got[i]
		}
	}
	if terminal == nil {
		t.Fatalf("no terminal failed run.status appended to run-interrupted")
	}
	if terminal.Seq != 3 {
		t.Fatalf("terminal seq = %d, want 3 (max+1)", terminal.Seq)
	}

	// run-done must not have gained a failed status.
	doneItems, err := timeline.ListRunTimelineItems(ctx, store.RunTimelineListOpts{RunID: "run-done", Limit: 10})
	if err != nil {
		t.Fatalf("List run-done: %v", err)
	}
	for _, it := range doneItems {
		if it.Status == store.RunTimelineStatusFailed {
			t.Fatalf("run-done wrongly marked failed at seq %d", it.Seq)
		}
	}

	// Idempotent: a second pass finds nothing new (run-interrupted now terminal).
	n2, err := timeline.RecoverInterruptedRuns(context.Background())
	if err != nil {
		t.Fatalf("RecoverInterruptedRuns (2nd): %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second recover = %d, want 0 (idempotent)", n2)
	}
}

func seedSQLiteRunTimelineTenant(t *testing.T, db execer, tenantID uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO tenants (id, name, slug, status)
		 VALUES (?, ?, ?, 'active')`,
		tenantID, "timeline-test-"+tenantID.String()[:8], "timeline-"+tenantID.String(),
	); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
}

func seedSQLiteRunTimelineAgent(t *testing.T, db execer, tenantID, agentID uuid.UUID) {
	t.Helper()
	seedSQLiteRunTimelineTenant(t, db, tenantID)
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES (?, ?, ?, 'predefined', 'active', 'test', 'test-model', 'owner')`,
		agentID, tenantID, "timeline-agent-"+agentID.String(),
	); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
}

type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

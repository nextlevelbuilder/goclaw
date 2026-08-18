//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestSQLiteRunStoreUpdateCheckpointRoundtrip writes a durable checkpoint, then
// reads it back along with the transitioned status.
func TestSQLiteRunStoreUpdateCheckpointRoundtrip(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	rs := NewSQLiteRunStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	run := store.AgentRun{RunID: "run-cp", SessionKey: "s", Status: store.AgentRunStatusRunning, StartedAt: time.Now()}
	if err := rs.CreateRun(ctx, &run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	cp := json.RawMessage(`{"version":1,"run_id":"run-cp","iteration":4}`)
	if err := rs.UpdateRunCheckpoint(ctx, "run-cp", store.AgentRunStatusCompacting, cp); err != nil {
		t.Fatalf("UpdateRunCheckpoint: %v", err)
	}

	got, err := rs.GetRun(ctx, "run-cp")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != store.AgentRunStatusCompacting {
		t.Fatalf("status = %s, want compacting", got.Status)
	}
	if string(got.Checkpoint) != string(cp) {
		t.Fatalf("checkpoint = %s, want %s", got.Checkpoint, cp)
	}
}

// TestSQLiteRunTimelineContentPersistedForNewTypes proves chunk/thinking/
// tool.started keep their full content while legacy types stay preview-only.
func TestSQLiteRunTimelineContentPersistedForNewTypes(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	timeline := NewSQLiteRunTimelineStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	items := []store.RunTimelineItem{
		{RunID: "run-cp-content", SessionKey: "s", Seq: 1,
			ItemType: store.RunTimelineItemTypeChunk, Title: "chunk", Content: "streamed delta"},
		{RunID: "run-cp-content", SessionKey: "s", Seq: 2,
			ItemType: store.RunTimelineItemTypeThinking, Title: "thinking", Content: "reasoning trace"},
		{RunID: "run-cp-content", SessionKey: "s", Seq: 3,
			ItemType: store.RunTimelineItemTypeToolStarted, Title: "tool", Content: `{"tool":"read_file"}`},
		{RunID: "run-cp-content", SessionKey: "s", Seq: 4,
			ItemType: store.RunTimelineItemTypeToolCall, Title: "legacy", Content: "must strip"},
	}
	for i := range items {
		if err := timeline.AppendRunTimelineItem(ctx, &items[i]); err != nil {
			t.Fatalf("AppendRunTimelineItem(%d): %v", i, err)
		}
	}

	got, err := timeline.ListRunTimelineItems(ctx, store.RunTimelineListOpts{RunID: "run-cp-content", Limit: 10})
	if err != nil {
		t.Fatalf("ListRunTimelineItems: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	checks := map[string]string{
		store.RunTimelineItemTypeChunk:       "streamed delta",
		store.RunTimelineItemTypeThinking:    "reasoning trace",
		store.RunTimelineItemTypeToolStarted: `{"tool":"read_file"}`,
		store.RunTimelineItemTypeToolCall:    "",
	}
	for _, it := range got {
		want, ok := checks[it.ItemType]
		if !ok {
			t.Fatalf("unexpected item_type %s", it.ItemType)
		}
		if it.Content != want {
			t.Fatalf("item_type %s content = %q, want %q", it.ItemType, it.Content, want)
		}
	}
}

// TestSQLiteRunStoreStaleRecoveryResumeAware proves stale runs WITH a valid
// checkpoint are paused (resumable) while stale runs without one stay failed.
func TestSQLiteRunStoreStaleRecoveryResumeAware(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	rs := NewSQLiteRunStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	old := time.Now().Add(-2 * time.Hour)
	staleWithCP := store.AgentRun{
		RunID: "run-stale-cp", SessionKey: "s", Status: store.AgentRunStatusRunning,
		StartedAt: old, HeartbeatAt: old,
	}
	if err := rs.CreateRun(ctx, &staleWithCP); err != nil {
		t.Fatalf("CreateRun(stale-with-cp): %v", err)
	}
	if err := rs.UpdateRunCheckpoint(ctx, "run-stale-cp", store.AgentRunStatusRunning, json.RawMessage(`{"iteration":3}`)); err != nil {
		t.Fatalf("UpdateRunCheckpoint(stale-with-cp): %v", err)
	}
	staleNoCP := store.AgentRun{
		RunID: "run-stale-nocp", SessionKey: "s", Status: store.AgentRunStatusRunning,
		StartedAt: old, HeartbeatAt: old,
	}
	if err := rs.CreateRun(ctx, &staleNoCP); err != nil {
		t.Fatalf("CreateRun(stale-no-cp): %v", err)
	}

	n, err := rs.RecoverStaleRuns(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("RecoverStaleRuns: %v", err)
	}
	if n != 2 {
		t.Fatalf("recovered = %d, want 2", n)
	}

	withCP, _ := rs.GetRun(ctx, "run-stale-cp")
	if withCP.Status != store.RunTimelineStatusPaused {
		t.Fatalf("stale-with-cp status = %s, want paused", withCP.Status)
	}
	if withCP.CompletedAt != nil {
		t.Fatal("stale-with-cp completed_at must stay NULL (resumable)")
	}
	if withCP.Error != "run paused: heartbeat expired, checkpoint available" {
		t.Fatalf("stale-with-cp error = %q", withCP.Error)
	}

	noCP, _ := rs.GetRun(ctx, "run-stale-nocp")
	if noCP.Status != store.AgentRunStatusFailed {
		t.Fatalf("stale-no-cp status = %s, want failed", noCP.Status)
	}
	if noCP.CompletedAt == nil {
		t.Fatal("stale-no-cp completed_at must be stamped (terminal)")
	}
}

// TestSQLiteRunTimelineRecoverInterruptedPaused proves a run with a valid
// checkpoint gets a paused run.status instead of a terminal-failed one.
func TestSQLiteRunTimelineRecoverInterruptedPaused(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	timeline := NewSQLiteRunTimelineStore(db)
	rs := NewSQLiteRunStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	add := func(item store.RunTimelineItem) {
		t.Helper()
		item.SessionKey = "agent:default:direct:user-1"
		if err := timeline.AppendRunTimelineItem(ctx, &item); err != nil {
			t.Fatalf("AppendRunTimelineItem(%s/%d): %v", item.RunID, item.Seq, err)
		}
	}

	// run-paused: started, no terminal, but agent_runs has a valid checkpoint.
	add(store.RunTimelineItem{RunID: "run-paused", Seq: 1, ItemType: store.RunTimelineItemTypeRunStatus, Status: store.RunTimelineStatusStarted, Title: "Run started"})
	if err := rs.CreateRun(ctx, &store.AgentRun{
		RunID: "run-paused", SessionKey: "agent:default:direct:user-1",
		Status: store.AgentRunStatusRunning, StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateRun(run-paused): %v", err)
	}
	if err := rs.UpdateRunCheckpoint(ctx, "run-paused", store.AgentRunStatusRunning, json.RawMessage(`{"iteration":2}`)); err != nil {
		t.Fatalf("UpdateRunCheckpoint(run-paused): %v", err)
	}

	// run-dead: started, no terminal, no checkpoint.
	add(store.RunTimelineItem{RunID: "run-dead", Seq: 1, ItemType: store.RunTimelineItemTypeRunStatus, Status: store.RunTimelineStatusStarted, Title: "Run started"})
	if err := rs.CreateRun(ctx, &store.AgentRun{
		RunID: "run-dead", SessionKey: "agent:default:direct:user-1",
		Status: store.AgentRunStatusRunning, StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateRun(run-dead): %v", err)
	}

	n, err := timeline.RecoverInterruptedRuns(context.Background())
	if err != nil {
		t.Fatalf("RecoverInterruptedRuns: %v", err)
	}
	if n != 2 {
		t.Fatalf("recovered = %d, want 2", n)
	}

	pausedItems, err := timeline.ListRunTimelineItems(ctx, store.RunTimelineListOpts{RunID: "run-paused", Limit: 10})
	if err != nil {
		t.Fatalf("List run-paused: %v", err)
	}
	var paused *store.RunTimelineItem
	for i := range pausedItems {
		if pausedItems[i].ItemType == store.RunTimelineItemTypeRunStatus &&
			pausedItems[i].Status == store.RunTimelineStatusPaused {
			paused = &pausedItems[i]
		}
	}
	if paused == nil {
		t.Fatal("no paused run.status appended to run-paused")
	}
	if paused.Preview != interruptedPausedPreview {
		t.Fatalf("paused preview = %q, want %q", paused.Preview, interruptedPausedPreview)
	}

	deadItems, err := timeline.ListRunTimelineItems(ctx, store.RunTimelineListOpts{RunID: "run-dead", Limit: 10})
	if err != nil {
		t.Fatalf("List run-dead: %v", err)
	}
	var failed *store.RunTimelineItem
	for i := range deadItems {
		if deadItems[i].ItemType == store.RunTimelineItemTypeRunStatus &&
			deadItems[i].Status == store.RunTimelineStatusFailed {
			failed = &deadItems[i]
		}
	}
	if failed == nil {
		t.Fatal("no failed run.status appended to run-dead")
	}

	// Idempotent: second pass finds nothing (paused counts as terminal now).
	n2, err := timeline.RecoverInterruptedRuns(context.Background())
	if err != nil {
		t.Fatalf("RecoverInterruptedRuns (2nd): %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second recover = %d, want 0", n2)
	}
}

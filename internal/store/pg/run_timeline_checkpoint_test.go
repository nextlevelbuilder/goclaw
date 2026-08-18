package pg

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestPGRunStoreUpdateCheckpointRoundtrip writes a durable checkpoint, then
// reads it back along with the transitioned status.
func TestPGRunStoreUpdateCheckpointRoundtrip(t *testing.T) {
	db := hooksTestDB(t)
	rs := NewPGRunStore(db)
	tenantID, _ := seedTenantAndAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	run := store.AgentRun{
		RunID: "pg-run-cp", SessionKey: "s", Status: store.AgentRunStatusRunning,
		StartedAt: time.Now(),
	}
	if err := rs.CreateRun(ctx, &run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	cp := json.RawMessage(`{"version":1,"run_id":"pg-run-cp","iteration":4}`)
	if err := rs.UpdateRunCheckpoint(ctx, "pg-run-cp", store.AgentRunStatusCompacting, cp); err != nil {
		t.Fatalf("UpdateRunCheckpoint: %v", err)
	}

	got, err := rs.GetRun(ctx, "pg-run-cp")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != store.AgentRunStatusCompacting {
		t.Fatalf("status = %s, want compacting", got.Status)
	}
	// JSONB does not guarantee key order, so compare semantically rather than
	// byte-for-byte (the stored text is normalized by Postgres).
	var gotCP, wantCP struct {
		Version   int    `json:"version"`
		RunID     string `json:"run_id"`
		Iteration int    `json:"iteration"`
	}
	if err := json.Unmarshal(got.Checkpoint, &gotCP); err != nil {
		t.Fatalf("unmarshal stored checkpoint: %v", err)
	}
	if err := json.Unmarshal(cp, &wantCP); err != nil {
		t.Fatalf("unmarshal want checkpoint: %v", err)
	}
	if gotCP != wantCP {
		t.Fatalf("checkpoint = %+v, want %+v", gotCP, wantCP)
	}
}

// TestPGRunStoreUpdateCheckpointTenantScope proves a checkpoint write is scoped
// to the context tenant.
func TestPGRunStoreUpdateCheckpointTenantScope(t *testing.T) {
	db := hooksTestDB(t)
	rs := NewPGRunStore(db)
	tenantID, _ := seedTenantAndAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	run := store.AgentRun{RunID: "pg-run-cp-scope", SessionKey: "s", Status: store.AgentRunStatusRunning}
	if err := rs.CreateRun(ctx, &run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	// No tenant context → fail closed (like every other tenant-scoped write).
	err := rs.UpdateRunCheckpoint(context.Background(), "pg-run-cp-scope", store.AgentRunStatusRunning, json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("UpdateRunCheckpoint with no tenant context succeeded, want fail-closed error")
	}
}

// TestPGRunTimelineContentPersistedForNewTypes proves chunk/thinking/tool.started
// keep their full content while legacy types stay preview-only.
func TestPGRunTimelineContentPersistedForNewTypes(t *testing.T) {
	db := hooksTestDB(t)
	timeline := NewPGRunTimelineStore(db)
	tenantID, agentID := seedTenantAndAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	items := []store.RunTimelineItem{
		{RunID: "pg-cp-content", SessionKey: "s", AgentID: &agentID, UserID: "u1", Seq: 1,
			ItemType: store.RunTimelineItemTypeChunk, Title: "chunk", Content: "streamed delta"},
		{RunID: "pg-cp-content", SessionKey: "s", AgentID: &agentID, UserID: "u1", Seq: 2,
			ItemType: store.RunTimelineItemTypeThinking, Title: "thinking", Content: "reasoning trace"},
		{RunID: "pg-cp-content", SessionKey: "s", AgentID: &agentID, UserID: "u1", Seq: 3,
			ItemType: store.RunTimelineItemTypeToolStarted, Title: "tool", Content: `{"tool":"read_file"}`},
		{RunID: "pg-cp-content", SessionKey: "s", AgentID: &agentID, UserID: "u1", Seq: 4,
			ItemType: store.RunTimelineItemTypeToolCall, Title: "legacy", Content: "must strip"},
	}
	for i := range items {
		if err := timeline.AppendRunTimelineItem(ctx, &items[i]); err != nil {
			t.Fatalf("AppendRunTimelineItem(%d): %v", i, err)
		}
	}

	got, err := timeline.ListRunTimelineItems(ctx, store.RunTimelineListOpts{RunID: "pg-cp-content", Limit: 10})
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

// TestPGRunTimelineRecoverInterruptedPaused proves a run with a valid
// agent_runs.checkpoint gets a paused run.status instead of terminal-failed.
func TestPGRunTimelineRecoverInterruptedPaused(t *testing.T) {
	db := hooksTestDB(t)
	timeline := NewPGRunTimelineStore(db)
	rs := NewPGRunStore(db)
	tenantID, _ := seedTenantAndAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	add := func(item store.RunTimelineItem) {
		t.Helper()
		item.SessionKey = "agent:default:direct:user-1"
		if err := timeline.AppendRunTimelineItem(ctx, &item); err != nil {
			t.Fatalf("AppendRunTimelineItem(%s/%d): %v", item.RunID, item.Seq, err)
		}
	}

	// run-paused: started, no terminal, agent_runs has a valid checkpoint.
	add(store.RunTimelineItem{RunID: "pg-paused", Seq: 1, ItemType: store.RunTimelineItemTypeRunStatus, Status: store.RunTimelineStatusStarted, Title: "Run started"})
	if err := rs.CreateRun(ctx, &store.AgentRun{
		RunID: "pg-paused", SessionKey: "agent:default:direct:user-1",
		Status: store.AgentRunStatusRunning, StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateRun(pg-paused): %v", err)
	}
	if err := rs.UpdateRunCheckpoint(ctx, "pg-paused", store.AgentRunStatusRunning, json.RawMessage(`{"iteration":2}`)); err != nil {
		t.Fatalf("UpdateRunCheckpoint(pg-paused): %v", err)
	}

	// run-dead: started, no terminal, no checkpoint.
	add(store.RunTimelineItem{RunID: "pg-dead", Seq: 1, ItemType: store.RunTimelineItemTypeRunStatus, Status: store.RunTimelineStatusStarted, Title: "Run started"})
	if err := rs.CreateRun(ctx, &store.AgentRun{
		RunID: "pg-dead", SessionKey: "agent:default:direct:user-1",
		Status: store.AgentRunStatusRunning, StartedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateRun(pg-dead): %v", err)
	}

	n, err := timeline.RecoverInterruptedRuns(context.Background())
	if err != nil {
		t.Fatalf("RecoverInterruptedRuns: %v", err)
	}
	if n != 2 {
		t.Fatalf("recovered = %d, want 2", n)
	}

	pausedItems, err := timeline.ListRunTimelineItems(ctx, store.RunTimelineListOpts{RunID: "pg-paused", Limit: 10})
	if err != nil {
		t.Fatalf("List pg-paused: %v", err)
	}
	var paused *store.RunTimelineItem
	for i := range pausedItems {
		if pausedItems[i].ItemType == store.RunTimelineItemTypeRunStatus &&
			pausedItems[i].Status == store.RunTimelineStatusPaused {
			paused = &pausedItems[i]
		}
	}
	if paused == nil {
		t.Fatal("no paused run.status appended to pg-paused")
	}
	if paused.Preview != interruptedPausedPreview {
		t.Fatalf("paused preview = %q, want %q", paused.Preview, interruptedPausedPreview)
	}

	deadItems, err := timeline.ListRunTimelineItems(ctx, store.RunTimelineListOpts{RunID: "pg-dead", Limit: 10})
	if err != nil {
		t.Fatalf("List pg-dead: %v", err)
	}
	var failed *store.RunTimelineItem
	for i := range deadItems {
		if deadItems[i].ItemType == store.RunTimelineItemTypeRunStatus &&
			deadItems[i].Status == store.RunTimelineStatusFailed {
			failed = &deadItems[i]
		}
	}
	if failed == nil {
		t.Fatal("no failed run.status appended to pg-dead")
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

// TestPGRunStoreStaleRecoveryResumeAware proves stale runs WITH a valid
// checkpoint are paused (resumable) while stale runs without one stay failed.
func TestPGRunStoreStaleRecoveryResumeAware(t *testing.T) {
	db := hooksTestDB(t)
	rs := NewPGRunStore(db)
	tenantID, _ := seedTenantAndAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	old := time.Now().Add(-2 * time.Hour)
	staleWithCP := store.AgentRun{
		RunID: "pg-run-stale-cp", SessionKey: "s", Status: store.AgentRunStatusRunning,
		StartedAt: old, HeartbeatAt: old,
	}
	if err := rs.CreateRun(ctx, &staleWithCP); err != nil {
		t.Fatalf("CreateRun(stale-with-cp): %v", err)
	}
	if err := rs.UpdateRunCheckpoint(ctx, "pg-run-stale-cp", store.AgentRunStatusRunning, json.RawMessage(`{"iteration":3}`)); err != nil {
		t.Fatalf("UpdateRunCheckpoint(stale-with-cp): %v", err)
	}
	staleNoCP := store.AgentRun{
		RunID: "pg-run-stale-nocp", SessionKey: "s", Status: store.AgentRunStatusRunning,
		StartedAt: old, HeartbeatAt: old,
	}
	if err := rs.CreateRun(ctx, &staleNoCP); err != nil {
		t.Fatalf("CreateRun(stale-no-cp): %v", err)
	}
	// Empty-string checkpoint must be treated as absent.
	staleEmptyCP := store.AgentRun{
		RunID: "pg-run-stale-empty", SessionKey: "s", Status: store.AgentRunStatusRunning,
		StartedAt: old, HeartbeatAt: old,
	}
	if err := rs.CreateRun(ctx, &staleEmptyCP); err != nil {
		t.Fatalf("CreateRun(stale-empty-cp): %v", err)
	}
	if err := rs.UpdateRunCheckpoint(ctx, "pg-run-stale-empty", store.AgentRunStatusRunning, nil); err != nil {
		t.Fatalf("UpdateRunCheckpoint(stale-empty-cp): %v", err)
	}

	n, err := rs.RecoverStaleRuns(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("RecoverStaleRuns: %v", err)
	}
	if n != 3 {
		t.Fatalf("recovered = %d, want 3", n)
	}

	withCP, _ := rs.GetRun(ctx, "pg-run-stale-cp")
	if withCP.Status != store.RunTimelineStatusPaused {
		t.Fatalf("stale-with-cp status = %s, want paused", withCP.Status)
	}
	if withCP.CompletedAt != nil {
		t.Fatal("stale-with-cp completed_at must stay NULL (resumable)")
	}
	if withCP.Error != "run paused: heartbeat expired, checkpoint available" {
		t.Fatalf("stale-with-cp error = %q", withCP.Error)
	}

	noCP, _ := rs.GetRun(ctx, "pg-run-stale-nocp")
	if noCP.Status != store.AgentRunStatusFailed {
		t.Fatalf("stale-no-cp status = %s, want failed", noCP.Status)
	}
	if noCP.CompletedAt == nil {
		t.Fatal("stale-no-cp completed_at must be stamped (terminal)")
	}

	emptyCP, _ := rs.GetRun(ctx, "pg-run-stale-empty")
	if emptyCP.Status != store.AgentRunStatusFailed {
		t.Fatalf("stale-empty-cp status = %s, want failed", emptyCP.Status)
	}
}

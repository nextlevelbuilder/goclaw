package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// snapshotsEqual compares two snapshot payloads semantically. PostgreSQL stores
// snapshot as JSONB, which normalizes whitespace / key formatting on output, so
// raw string comparison would spuriously fail.
func snapshotsEqual(t *testing.T, got, want []byte) bool {
	t.Helper()
	var gotJSON, wantJSON any
	if err := json.Unmarshal(got, &gotJSON); err != nil {
		t.Fatalf("parse got snapshot: %v", err)
	}
	if err := json.Unmarshal(want, &wantJSON); err != nil {
		t.Fatalf("parse want snapshot: %v", err)
	}
	return reflect.DeepEqual(gotJSON, wantJSON)
}

func TestPGCheckpointSnapshotRoundtrip(t *testing.T) {
	db := hooksTestDB(t)
	cs := NewPGCheckpointSnapshotStore(db)
	tenantID, _ := seedTenantAndAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	snap := store.CheckpointSnapshot{
		RunID:     "pg-snap-run-1",
		Seq:       3,
		Status:    store.CheckpointSnapshotCompacting,
		Snapshot:  json.RawMessage(`{"version":1,"run_id":"pg-snap-run-1","iteration":2,"messages":[]}`),
		Iteration: 2,
	}
	if err := cs.AppendCheckpointSnapshot(ctx, &snap); err != nil {
		t.Fatalf("AppendCheckpointSnapshot: %v", err)
	}
	if snap.ID == uuid.Nil {
		t.Fatal("AppendCheckpointSnapshot did not assign ID")
	}
	if snap.TenantID != tenantID {
		t.Fatalf("tenant_id = %v, want %v", snap.TenantID, tenantID)
	}

	got, err := cs.GetCheckpointSnapshot(ctx, "pg-snap-run-1", 3)
	if err != nil {
		t.Fatalf("GetCheckpointSnapshot: %v", err)
	}
	if got.RunID != "pg-snap-run-1" || got.Seq != 3 || got.Iteration != 2 {
		t.Fatalf("got = %+v, want run/seq/iteration match", got)
	}
	if got.Status != store.CheckpointSnapshotCompacting {
		t.Fatalf("status = %q, want compacting", got.Status)
	}
	if !snapshotsEqual(t, got.Snapshot, snap.Snapshot) {
		t.Fatalf("snapshot roundtrip mismatch: got %s, want %s", got.Snapshot, snap.Snapshot)
	}
}

func TestPGCheckpointSnapshotListNewestFirst(t *testing.T) {
	db := hooksTestDB(t)
	cs := NewPGCheckpointSnapshotStore(db)
	tenantID, _ := seedTenantAndAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	for _, seq := range []int{1, 2, 3, 4} {
		snap := store.CheckpointSnapshot{
			RunID:     "pg-snap-list",
			Seq:       seq,
			Status:    store.CheckpointSnapshotPaused,
			Snapshot:  json.RawMessage(fmt.Sprintf(`{"seq":%d}`, seq)),
			Iteration: seq,
		}
		if err := cs.AppendCheckpointSnapshot(ctx, &snap); err != nil {
			t.Fatalf("AppendCheckpointSnapshot(%d): %v", seq, err)
		}
	}

	got, err := cs.ListCheckpointSnapshots(ctx, store.CheckpointSnapshotListOpts{RunID: "pg-snap-list", Limit: 10})
	if err != nil {
		t.Fatalf("ListCheckpointSnapshots: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	for i := 0; i < len(got); i++ {
		if got[i].Seq != 4-i {
			t.Fatalf("order[%d] seq = %d, want %d (newest first)", i, got[i].Seq, 4-i)
		}
	}
}

func TestPGCheckpointSnapshotListRunScoped(t *testing.T) {
	db := hooksTestDB(t)
	cs := NewPGCheckpointSnapshotStore(db)
	tenantID, _ := seedTenantAndAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	for _, runID := range []string{"pg-a", "pg-b"} {
		snap := store.CheckpointSnapshot{
			RunID:     runID,
			Seq:       1,
			Status:    store.CheckpointSnapshotRunning,
			Snapshot:  json.RawMessage(`{}`),
			Iteration: 1,
		}
		if err := cs.AppendCheckpointSnapshot(ctx, &snap); err != nil {
			t.Fatalf("AppendCheckpointSnapshot(%s): %v", runID, err)
		}
	}

	gotA, err := cs.ListCheckpointSnapshots(ctx, store.CheckpointSnapshotListOpts{RunID: "pg-a", Limit: 10})
	if err != nil {
		t.Fatalf("ListCheckpointSnapshots(pg-a): %v", err)
	}
	if len(gotA) != 1 || gotA[0].RunID != "pg-a" {
		t.Fatalf("pg-a list = %+v, want single pg-a snapshot", gotA)
	}
}

func TestPGCheckpointSnapshotDefaultStatus(t *testing.T) {
	db := hooksTestDB(t)
	cs := NewPGCheckpointSnapshotStore(db)
	tenantID, _ := seedTenantAndAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	snap := store.CheckpointSnapshot{RunID: "pg-snap-default", Seq: 1, Snapshot: json.RawMessage(`{}`)}
	if err := cs.AppendCheckpointSnapshot(ctx, &snap); err != nil {
		t.Fatalf("AppendCheckpointSnapshot: %v", err)
	}
	if snap.Status != store.CheckpointSnapshotPaused {
		t.Fatalf("default status = %q, want paused", snap.Status)
	}

	if err := cs.AppendCheckpointSnapshot(ctx, &store.CheckpointSnapshot{
		RunID: "pg-snap-bad", Seq: 1, Status: "bogus", Snapshot: json.RawMessage(`{}`),
	}); err == nil {
		t.Fatal("AppendCheckpointSnapshot with invalid status succeeded, want error")
	}
}

func TestPGCheckpointSnapshotTenantScope(t *testing.T) {
	db := hooksTestDB(t)
	cs := NewPGCheckpointSnapshotStore(db)
	tenantID, _ := seedTenantAndAgent(t, db)
	ctxA := store.WithTenantID(context.Background(), tenantID)
	ctxB := store.WithTenantID(context.Background(), uuid.Must(uuid.NewV7()))

	snap := store.CheckpointSnapshot{RunID: "pg-snap-scope", Seq: 1, Snapshot: json.RawMessage(`{"scope":"a"}`)}
	if err := cs.AppendCheckpointSnapshot(ctxA, &snap); err != nil {
		t.Fatalf("AppendCheckpointSnapshot tenant A: %v", err)
	}

	// Tenant B get must fail closed.
	gotB, err := cs.GetCheckpointSnapshot(ctxB, "pg-snap-scope", 1)
	if err == nil {
		t.Fatalf("tenant B GetCheckpointSnapshot succeeded with %+v, want error", gotB)
	}
	// No-tenant get must fail closed.
	gotNoTenant, err := cs.GetCheckpointSnapshot(context.Background(), "pg-snap-scope", 1)
	if err == nil {
		t.Fatalf("no-tenant GetCheckpointSnapshot succeeded with %+v, want fail-closed error", gotNoTenant)
	}

	listB, err := cs.ListCheckpointSnapshots(ctxB, store.CheckpointSnapshotListOpts{RunID: "pg-snap-scope", Limit: 10})
	if err != nil {
		t.Fatalf("ListCheckpointSnapshots tenant B: %v", err)
	}
	if len(listB) != 0 {
		t.Fatalf("tenant B list len = %d, want 0", len(listB))
	}
}
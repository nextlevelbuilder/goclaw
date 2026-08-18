//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func TestSQLiteCheckpointSnapshotRoundtrip(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	cs := NewSQLiteCheckpointSnapshotStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	snap := store.CheckpointSnapshot{
		RunID:     "sqlite-snap-run-1",
		Seq:       3,
		Status:    store.CheckpointSnapshotCompacting,
		Snapshot:  json.RawMessage(`{"version":1,"run_id":"sqlite-snap-run-1","iteration":2,"messages":[]}`),
		Iteration: 2,
	}
	if err := cs.AppendCheckpointSnapshot(ctx, &snap); err != nil {
		t.Fatalf("AppendCheckpointSnapshot: %v", err)
	}
	if snap.ID == uuid.Nil {
		t.Fatal("AppendCheckpointSnapshot did not assign ID")
	}
	if snap.TenantID != store.MasterTenantID {
		t.Fatalf("tenant_id = %v, want master %v", snap.TenantID, store.MasterTenantID)
	}

	got, err := cs.GetCheckpointSnapshot(ctx, "sqlite-snap-run-1", 3)
	if err != nil {
		t.Fatalf("GetCheckpointSnapshot: %v", err)
	}
	if got.RunID != "sqlite-snap-run-1" || got.Seq != 3 || got.Iteration != 2 {
		t.Fatalf("got = %+v, want run/seq/iteration match", got)
	}
	if got.Status != store.CheckpointSnapshotCompacting {
		t.Fatalf("status = %q, want compacting", got.Status)
	}
	if string(got.Snapshot) != string(snap.Snapshot) {
		t.Fatalf("snapshot roundtrip mismatch: got %s, want %s", got.Snapshot, snap.Snapshot)
	}
}

func TestSQLiteCheckpointSnapshotListNewestFirst(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	cs := NewSQLiteCheckpointSnapshotStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	for _, seq := range []int{1, 2, 3, 4} {
		snap := store.CheckpointSnapshot{
			RunID:     "sqlite-snap-list",
			Seq:       seq,
			Status:    store.CheckpointSnapshotPaused,
			Snapshot:  json.RawMessage(fmt.Sprintf(`{"seq":%d}`, seq)),
			Iteration: seq,
		}
		if err := cs.AppendCheckpointSnapshot(ctx, &snap); err != nil {
			t.Fatalf("AppendCheckpointSnapshot(%d): %v", seq, err)
		}
	}

	got, err := cs.ListCheckpointSnapshots(ctx, store.CheckpointSnapshotListOpts{RunID: "sqlite-snap-list", Limit: 10})
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

func TestSQLiteCheckpointSnapshotListRunScoped(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	cs := NewSQLiteCheckpointSnapshotStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	for _, runID := range []string{"sqlite-a", "sqlite-b"} {
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

	gotA, err := cs.ListCheckpointSnapshots(ctx, store.CheckpointSnapshotListOpts{RunID: "sqlite-a", Limit: 10})
	if err != nil {
		t.Fatalf("ListCheckpointSnapshots(sqlite-a): %v", err)
	}
	if len(gotA) != 1 || gotA[0].RunID != "sqlite-a" {
		t.Fatalf("sqlite-a list = %+v, want single sqlite-a snapshot", gotA)
	}
}

func TestSQLiteCheckpointSnapshotDefaultStatus(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	cs := NewSQLiteCheckpointSnapshotStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	snap := store.CheckpointSnapshot{RunID: "sqlite-snap-default", Seq: 1, Snapshot: json.RawMessage(`{}`)}
	if err := cs.AppendCheckpointSnapshot(ctx, &snap); err != nil {
		t.Fatalf("AppendCheckpointSnapshot: %v", err)
	}
	if snap.Status != store.CheckpointSnapshotPaused {
		t.Fatalf("default status = %q, want paused", snap.Status)
	}

	if err := cs.AppendCheckpointSnapshot(ctx, &store.CheckpointSnapshot{
		RunID: "sqlite-snap-bad", Seq: 1, Status: "bogus", Snapshot: json.RawMessage(`{}`),
	}); err == nil {
		t.Fatal("AppendCheckpointSnapshot with invalid status succeeded, want error")
	}
}

func TestSQLiteCheckpointSnapshotTenantScope(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	cs := NewSQLiteCheckpointSnapshotStore(db)
	tenantA := uuid.Must(uuid.NewV7())
	tenantB := uuid.Must(uuid.NewV7())
	seedSQLiteRunTimelineTenant(t, db, tenantA)
	seedSQLiteRunTimelineTenant(t, db, tenantB)
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	snap := store.CheckpointSnapshot{RunID: "sqlite-snap-scope", Seq: 1, Snapshot: json.RawMessage(`{"scope":"a"}`)}
	if err := cs.AppendCheckpointSnapshot(ctxA, &snap); err != nil {
		t.Fatalf("AppendCheckpointSnapshot tenant A: %v", err)
	}

	gotB, err := cs.GetCheckpointSnapshot(ctxB, "sqlite-snap-scope", 1)
	if err == nil {
		t.Fatalf("tenant B GetCheckpointSnapshot succeeded with %+v, want error", gotB)
	}
	gotNoTenant, err := cs.GetCheckpointSnapshot(context.Background(), "sqlite-snap-scope", 1)
	if err == nil {
		t.Fatalf("no-tenant GetCheckpointSnapshot succeeded with %+v, want fail-closed error", gotNoTenant)
	}

	listB, err := cs.ListCheckpointSnapshots(ctxB, store.CheckpointSnapshotListOpts{RunID: "sqlite-snap-scope", Limit: 10})
	if err != nil {
		t.Fatalf("ListCheckpointSnapshots tenant B: %v", err)
	}
	if len(listB) != 0 {
		t.Fatalf("tenant B list len = %d, want 0", len(listB))
	}
}
//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestSQLiteRunStoreCreateGetUpdate terminally exercises the durable run
// state machine record: create → running → compacting → completed, reading
// back the mutated fields after each transition.
func TestSQLiteRunStoreCreateGetUpdate(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	rs := NewSQLiteRunStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	now := time.Now()
	run := store.AgentRun{
		RunID:      "run-1",
		SessionKey: "agent:default:direct:user-1",
		AgentID:    nil,
		UserID:     "user-1",
		Channel:    "web",
		ChatID:     "chat-1",
		Status:     store.AgentRunStatusPending,
		Attempt:    1,
		Metadata:   json.RawMessage(`{"customer":"acme"}`),
		StartedAt:  now,
	}
	if err := rs.CreateRun(ctx, &run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.ID == uuid.Nil {
		t.Fatal("CreateRun did not assign ID")
	}
	if run.TenantID != store.MasterTenantID {
		t.Fatalf("TenantID = %s, want master", run.TenantID)
	}

	got, err := rs.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != store.AgentRunStatusPending {
		t.Fatalf("status = %s, want pending", got.Status)
	}
	if got.Attempt != 1 {
		t.Fatalf("attempt = %d, want 1", got.Attempt)
	}
	if string(got.Metadata) != `{"customer":"acme"}` {
		t.Fatalf("metadata = %s", got.Metadata)
	}

	if err := rs.UpdateRunStatus(ctx, "run-1", store.AgentRunStatusRunning); err != nil {
		t.Fatalf("UpdateRunStatus(running): %v", err)
	}
	got, err = rs.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun after running: %v", err)
	}
	if got.Status != store.AgentRunStatusRunning {
		t.Fatalf("status = %s, want running", got.Status)
	}
	if got.CompletedAt != nil {
		t.Fatal("CompletedAt set on a non-terminal transition")
	}

	if err := rs.UpdateRunStatus(ctx, "run-1", store.AgentRunStatusCompacting); err != nil {
		t.Fatalf("UpdateRunStatus(compacting): %v", err)
	}

	completedAt := time.Now().Add(time.Minute)
	if err := rs.UpdateRunTerminal(ctx, "run-1", store.AgentRunStatusCompleted, "", completedAt); err != nil {
		t.Fatalf("UpdateRunTerminal: %v", err)
	}
	got, err = rs.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun after completed: %v", err)
	}
	if got.Status != store.AgentRunStatusCompleted {
		t.Fatalf("status = %s, want completed", got.Status)
	}
	if got.CompletedAt == nil || !got.CompletedAt.Equal(completedAt) {
		t.Fatalf("CompletedAt = %v, want %v", got.CompletedAt, completedAt)
	}
}

// TestSQLiteRunStoreHeartbeat advances heartbeat_at and confirms the change
// lands on the record.
func TestSQLiteRunStoreHeartbeat(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	rs := NewSQLiteRunStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	before := time.Now().Add(-time.Hour)
	run := store.AgentRun{RunID: "run-hb", SessionKey: "s", Status: store.AgentRunStatusRunning, StartedAt: before}
	if err := rs.CreateRun(ctx, &run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	// Force an old heartbeat_at to prove TouchHeartbeat really moves it forward.
	time.Sleep(5 * time.Millisecond)
	if err := rs.TouchHeartbeat(ctx, "run-hb"); err != nil {
		t.Fatalf("TouchHeartbeat: %v", err)
	}
	got, err := rs.GetRun(ctx, "run-hb")
	if err != nil {
		t.Fatalf("GetRun after heartbeat: %v", err)
	}
	if got.HeartbeatAt.Before(before) {
		t.Fatalf("heartbeat_at = %v, want after %v", got.HeartbeatAt, before)
	}
}

// TestSQLiteRunStoreStaleRecovery marks an old running run as failed while
// leaving a fresh one untouched, and proves recover is idempotent.
func TestSQLiteRunStoreStaleRecovery(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	rs := NewSQLiteRunStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	stale := store.AgentRun{
		RunID: "run-stale", SessionKey: "s", Status: store.AgentRunStatusRunning,
		StartedAt: time.Now().Add(-2 * time.Hour), HeartbeatAt: time.Now().Add(-2 * time.Hour),
	}
	fresh := store.AgentRun{
		RunID: "run-fresh", SessionKey: "s", Status: store.AgentRunStatusRunning,
		StartedAt: time.Now(), HeartbeatAt: time.Now(),
	}
	done := store.AgentRun{
		RunID: "run-done", SessionKey: "s", Status: store.AgentRunStatusCompleted,
		StartedAt: time.Now().Add(-2 * time.Hour), HeartbeatAt: time.Now().Add(-2 * time.Hour),
	}
	for _, r := range []*store.AgentRun{&stale, &fresh, &done} {
		if err := rs.CreateRun(ctx, r); err != nil {
			t.Fatalf("CreateRun(%s): %v", r.RunID, err)
		}
	}

	n, err := rs.RecoverStaleRuns(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("RecoverStaleRuns: %v", err)
	}
	if n != 1 {
		t.Fatalf("recovered = %d, want 1 (only run-stale)", n)
	}

	gotStale, err := rs.GetRun(ctx, "run-stale")
	if err != nil {
		t.Fatalf("GetRun run-stale: %v", err)
	}
	if gotStale.Status != store.AgentRunStatusFailed {
		t.Fatalf("run-stale status = %s, want failed", gotStale.Status)
	}
	gotFresh, err := rs.GetRun(ctx, "run-fresh")
	if err != nil {
		t.Fatalf("GetRun run-fresh: %v", err)
	}
	if gotFresh.Status != store.AgentRunStatusRunning {
		t.Fatalf("run-fresh status = %s, want running (untouched)", gotFresh.Status)
	}
	gotDone, err := rs.GetRun(ctx, "run-done")
	if err != nil {
		t.Fatalf("GetRun run-done: %v", err)
	}
	if gotDone.Status != store.AgentRunStatusCompleted {
		t.Fatalf("run-done status = %s, want completed (untouched)", gotDone.Status)
	}

	// Idempotent: retired run is no longer 'running', so a second pass is a no-op.
	n2, err := rs.RecoverStaleRuns(context.Background(), time.Hour)
	if err != nil {
		t.Fatalf("RecoverStaleRuns (2nd): %v", err)
	}
	if n2 != 0 {
		t.Fatalf("second recover = %d, want 0", n2)
	}
}

// TestSQLiteRunStoreTenantIsolation proves tenant B cannot read tenant A's
// run record, and that a no-tenant context fails closed.
func TestSQLiteRunStoreTenantIsolation(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	rs := NewSQLiteRunStore(db)
	tenantA := uuid.Must(uuid.NewV7())
	tenantB := uuid.Must(uuid.NewV7())
	seedSQLiteRunTimelineTenant(t, db, tenantA)
	seedSQLiteRunTimelineTenant(t, db, tenantB)
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	run := store.AgentRun{RunID: "run-shared", SessionKey: "s", Status: store.AgentRunStatusRunning}
	if err := rs.CreateRun(ctxA, &run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.TenantID != tenantA {
		t.Fatalf("TenantID = %s, want %s", run.TenantID, tenantA)
	}

	gotA, err := rs.GetRun(ctxA, "run-shared")
	if err != nil || gotA == nil {
		t.Fatalf("GetRun tenant A: %v", err)
	}

	gotB, err := rs.GetRun(ctxB, "run-shared")
	if err == nil {
		t.Fatalf("tenant B GetRun succeeded with %+v, want error", gotB)
	}

	gotNoTenant, err := rs.GetRun(context.Background(), "run-shared")
	if err == nil {
		t.Fatalf("no-tenant GetRun succeeded with %+v, want fail-closed error", gotNoTenant)
	}

	// List is also tenant-scoped: B sees nothing.
	listB, err := rs.ListRuns(ctxB, store.RunListOpts{SessionKey: "s"})
	if err != nil {
		t.Fatalf("ListRuns tenant B: %v", err)
	}
	if len(listB) != 0 {
		t.Fatalf("tenant B list len = %d, want 0", len(listB))
	}
}

// TestSQLiteRunStoreListFilters exercises keyword filters and ordering.
func TestSQLiteRunStoreListFilters(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	rs := NewSQLiteRunStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	runs := []store.AgentRun{
		{RunID: "r1", SessionKey: "s-ending", Status: store.AgentRunStatusRunning},
		{RunID: "r2", SessionKey: "s-ending", Status: store.AgentRunStatusFailed},
		{RunID: "r3", SessionKey: "s-other", Status: store.AgentRunStatusCompleted},
	}
	for i := range runs {
		if err := rs.CreateRun(ctx, &runs[i]); err != nil {
			t.Fatalf("CreateRun(%s): %v", runs[i].RunID, err)
		}
	}

	all, err := rs.ListRuns(ctx, store.RunListOpts{SessionKey: "s-ending", Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("list len = %d, want 2", len(all))
	}

	running, err := rs.ListRuns(ctx, store.RunListOpts{SessionKey: "s-ending", Status: store.AgentRunStatusRunning})
	if err != nil {
		t.Fatalf("ListRuns(running): %v", err)
	}
	if len(running) != 1 || running[0].RunID != "r1" {
		t.Fatalf("running filter = %+v, want only r1", running)
	}

	byRun, err := rs.ListRuns(ctx, store.RunListOpts{RunID: "r3"})
	if err != nil {
		t.Fatalf("ListRuns(runID): %v", err)
	}
	if len(byRun) != 1 || byRun[0].SessionKey != "s-other" {
		t.Fatalf("runID filter = %+v, want only r3", byRun)
	}

	// Newest-first ordering within the s-ending group: r2 (created after r1) first.
	if all[0].RunID != "r2" {
		t.Fatalf("order = %s first, want r2 (newest desc within filter)", all[0].RunID)
	}

	// Unfiltered list across the session keys includes r3.
	all3, err := rs.ListRuns(ctx, store.RunListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns(all): %v", err)
	}
	if len(all3) != 3 {
		t.Fatalf("all list len = %d, want 3", len(all3))
	}
	if all3[0].RunID != "r3" {
		t.Fatalf("all order = %s first, want r3 (newest desc)", all3[0].RunID)
	}
}

// TestSQLiteRunStoreTerminalError persists an error message on the failed path.
func TestSQLiteRunStoreTerminalError(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	rs := NewSQLiteRunStore(db)
	ctx := store.WithTenantID(context.Background(), store.MasterTenantID)
	seedSQLiteRunTimelineTenant(t, db, store.MasterTenantID)

	run := store.AgentRun{RunID: "run-err", SessionKey: "s", Status: store.AgentRunStatusRunning}
	if err := rs.CreateRun(ctx, &run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := rs.UpdateRunTerminal(ctx, "run-err", store.AgentRunStatusFailed, "provider timeout", time.Now()); err != nil {
		t.Fatalf("UpdateRunTerminal: %v", err)
	}
	got, err := rs.GetRun(ctx, "run-err")
	if err != nil {
		t.Fatalf("GetRun run-err: %v", err)
	}
	if got.Error != "provider timeout" {
		t.Fatalf("error = %q, want provider timeout", got.Error)
	}
	if got.CompletedAt == nil {
		t.Fatal("CompletedAt not set on failure")
	}
}
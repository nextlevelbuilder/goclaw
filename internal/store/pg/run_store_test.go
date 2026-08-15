package pg

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// TestPGRunStoreCreateGetUpdateTerminal exercises the durable run state
// machine record life cycle on PostgreSQL.
func TestPGRunStoreCreateGetUpdateTerminal(t *testing.T) {
	db := hooksTestDB(t)
	rs := NewPGRunStore(db)
	tenantID, _ := seedTenantAndAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	now := time.Now()
	run := store.AgentRun{
		RunID:      "pg-run-1",
		SessionKey: "agent:default:direct:user-1",
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
	if run.TenantID != tenantID {
		t.Fatalf("TenantID = %s, want %s", run.TenantID, tenantID)
	}

	got, err := rs.GetRun(ctx, "pg-run-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != store.AgentRunStatusPending {
		t.Fatalf("status = %s, want pending", got.Status)
	}
	if got.Attempt != 1 {
		t.Fatalf("attempt = %d, want 1", got.Attempt)
	}
	var md map[string]any
	if err := json.Unmarshal(got.Metadata, &md); err != nil {
		t.Fatalf("metadata %q not valid JSON: %v", got.Metadata, err)
	}
	if md["customer"] != "acme" {
		t.Fatalf("metadata = %s, want customer=acme", got.Metadata)
	}

	if err := rs.UpdateRunStatus(ctx, "pg-run-1", store.AgentRunStatusRunning); err != nil {
		t.Fatalf("UpdateRunStatus(running): %v", err)
	}
	if err := rs.UpdateRunStatus(ctx, "pg-run-1", store.AgentRunStatusCompacting); err != nil {
		t.Fatalf("UpdateRunStatus(compacting): %v", err)
	}

	completedAt := time.Now().Add(time.Minute)
	if err := rs.UpdateRunTerminal(ctx, "pg-run-1", store.AgentRunStatusCompleted, "", completedAt); err != nil {
		t.Fatalf("UpdateRunTerminal: %v", err)
	}
	got, _ = rs.GetRun(ctx, "pg-run-1")
	if got.Status != store.AgentRunStatusCompleted {
		t.Fatalf("status = %s, want completed", got.Status)
	}
	if got.CompletedAt == nil ||
		got.CompletedAt.Sub(completedAt) > time.Millisecond || completedAt.Sub(*got.CompletedAt) > time.Millisecond {
		t.Fatalf("CompletedAt = %v, want %v (within 1ms)", got.CompletedAt, completedAt)
	}
}

// TestPGRunStoreHeartbeat advances heartbeat_at.
func TestPGRunStoreHeartbeat(t *testing.T) {
	db := hooksTestDB(t)
	rs := NewPGRunStore(db)
	tenantID, _ := seedTenantAndAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	before := time.Now().Add(-time.Hour)
	run := store.AgentRun{RunID: "pg-run-hb", SessionKey: "s", Status: store.AgentRunStatusRunning, StartedAt: before}
	run.HeartbeatAt = before
	if err := rs.CreateRun(ctx, &run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := rs.TouchHeartbeat(ctx, "pg-run-hb"); err != nil {
		t.Fatalf("TouchHeartbeat: %v", err)
	}
	got, err := rs.GetRun(ctx, "pg-run-hb")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if !got.HeartbeatAt.After(before) {
		t.Fatalf("heartbeat_at = %v, want after %v", got.HeartbeatAt, before)
	}
}

// TestPGRunStoreStaleRecovery marks only the stale running run as failed.
func TestPGRunStoreStaleRecovery(t *testing.T) {
	db := hooksTestDB(t)
	rs := NewPGRunStore(db)
	tenantID, _ := seedTenantAndAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	stale := store.AgentRun{
		RunID: "pg-run-stale", SessionKey: "s", Status: store.AgentRunStatusRunning,
		StartedAt: time.Now().Add(-2 * time.Hour), HeartbeatAt: time.Now().Add(-2 * time.Hour),
	}
	fresh := store.AgentRun{
		RunID: "pg-run-fresh", SessionKey: "s", Status: store.AgentRunStatusRunning,
		StartedAt: time.Now(), HeartbeatAt: time.Now(),
	}
	done := store.AgentRun{
		RunID: "pg-run-done", SessionKey: "s", Status: store.AgentRunStatusCompleted,
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
		t.Fatalf("recovered = %d, want 1", n)
	}

	gotStale, err := rs.GetRun(ctx, "pg-run-stale")
	if err != nil {
		t.Fatalf("GetRun stale: %v", err)
	}
	if gotStale.Status != store.AgentRunStatusFailed {
		t.Fatalf("run-stale status = %s, want failed", gotStale.Status)
	}
	gotFresh, _ := rs.GetRun(ctx, "pg-run-fresh")
	if gotFresh.Status != store.AgentRunStatusRunning {
		t.Fatalf("run-fresh status = %s, want running", gotFresh.Status)
	}
	gotDone, _ := rs.GetRun(ctx, "pg-run-done")
	if gotDone.Status != store.AgentRunStatusCompleted {
		t.Fatalf("run-done status = %s, want completed", gotDone.Status)
	}
}

// TestPGRunStoreTenantIsolation proves tenant B cannot read tenant A's run.
func TestPGRunStoreTenantIsolation(t *testing.T) {
	db := hooksTestDB(t)
	rs := NewPGRunStore(db)
	tenantA, _ := seedTenantAndAgent(t, db)
	tenantB, _ := seedTenantAndAgent(t, db)
	ctxA := store.WithTenantID(context.Background(), tenantA)
	ctxB := store.WithTenantID(context.Background(), tenantB)

	run := store.AgentRun{RunID: "pg-run-shared", SessionKey: "s", Status: store.AgentRunStatusRunning}
	if err := rs.CreateRun(ctxA, &run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run.TenantID != tenantA {
		t.Fatalf("TenantID = %s, want %s", run.TenantID, tenantA)
	}

	if _, err := rs.GetRun(ctxB, "pg-run-shared"); err == nil {
		t.Fatal("tenant B GetRun succeeded, want error")
	}
	if _, err := rs.GetRun(context.Background(), "pg-run-shared"); err == nil {
		t.Fatal("no-tenant GetRun succeeded, want fail-closed error")
	}

	listB, err := rs.ListRuns(ctxB, store.RunListOpts{SessionKey: "s"})
	if err != nil {
		t.Fatalf("ListRuns B: %v", err)
	}
	if len(listB) != 0 {
		t.Fatalf("tenant B list len = %d, want 0", len(listB))
	}
}

// TestPGRunStoreListFilters exercises keyword filters + newest-first order.
func TestPGRunStoreListFilters(t *testing.T) {
	db := hooksTestDB(t)
	rs := NewPGRunStore(db)
	tenantID, _ := seedTenantAndAgent(t, db)
	ctx := store.WithTenantID(context.Background(), tenantID)

	base := time.Now()
	for i, r := range []store.AgentRun{
		{RunID: "pg-r1", SessionKey: "s-ending", Status: store.AgentRunStatusRunning, StartedAt: base},
		{RunID: "pg-r2", SessionKey: "s-ending", Status: store.AgentRunStatusFailed, StartedAt: base.Add(1 * time.Second)},
		{RunID: "pg-r3", SessionKey: "s-other", Status: store.AgentRunStatusCompleted, StartedAt: base.Add(2 * time.Second)},
	} {
		if err := rs.CreateRun(ctx, &r); err != nil {
			t.Fatalf("CreateRun(%d): %v", i, err)
		}
	}

	all, err := rs.ListRuns(ctx, store.RunListOpts{SessionKey: "s-ending", Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("list len = %d, want 2", len(all))
	}
	if all[0].RunID != "pg-r2" {
		t.Fatalf("order = %s first, want pg-r2 (newest desc)", all[0].RunID)
	}

	running, err := rs.ListRuns(ctx, store.RunListOpts{SessionKey: "s-ending", Status: store.AgentRunStatusRunning})
	if err != nil {
		t.Fatalf("ListRuns(running): %v", err)
	}
	if len(running) != 1 || running[0].RunID != "pg-r1" {
		t.Fatalf("running filter = %+v, want only pg-r1", running)
	}

	// Newest-first across all: pg-r3 created last.
	all3, err := rs.ListRuns(ctx, store.RunListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListRuns(all): %v", err)
	}
	if len(all3) != 3 {
		t.Fatalf("all list len = %d, want 3", len(all3))
	}
	if all3[0].RunID != "pg-r3" {
		t.Fatalf("all order = %s first, want pg-r3", all3[0].RunID)
	}
}
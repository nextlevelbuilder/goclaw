//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// seedSQLiteMissionTenant inserts a tenant + agent and registers cleanup.
func seedSQLiteMissionTenant(t *testing.T, db execer, tenantID, agentID uuid.UUID) {
	t.Helper()
	seedSQLiteRunTimelineTenant(t, db, tenantID)
	if _, err := db.Exec(
		`INSERT OR IGNORE INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES (?, ?, ?, 'predefined', 'active', 'test', 'test-model', 'owner')`,
		agentID, tenantID, "mission-agent-"+agentID.String(),
	); err != nil {
		t.Fatalf("seed mission agent: %v", err)
	}
}

func missionCtx(tenantID uuid.UUID) context.Context {
	return store.WithTenantID(context.Background(), tenantID)
}

func TestSQLiteMissionStoreCreateGetRoundtrip(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	tenantID := uuid.Must(uuid.NewV7())
	agentID := uuid.Must(uuid.NewV7())
	seedSQLiteMissionTenant(t, db, tenantID, agentID)

	ms := NewSQLiteMissionStore(db)
	ctx := missionCtx(tenantID)

	m := store.Mission{
		Name:       "Launch readiness",
		Goals:      []string{"Ship v1"},
		Milestones: []string{"Design", "Build"},
		Acceptance: []string{"All tests green"},
		AgentID:    &agentID,
		SessionKey: "agent:mission-launch:s1",
		Metadata:   json.RawMessage(`{"channel":"cron"}`),
	}
	if err := ms.CreateMission(ctx, &m); err != nil {
		t.Fatalf("CreateMission: %v", err)
	}
	if m.ID == uuid.Nil {
		t.Fatal("CreateMission did not assign ID")
	}
	if m.Status != store.MissionStatusActive {
		t.Fatalf("status = %q, want default active", m.Status)
	}
	if m.TenantID != tenantID {
		t.Fatalf("tenant_id = %v, want %v", m.TenantID, tenantID)
	}

	got, err := ms.GetMission(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetMission: %v", err)
	}
	if got.Name != "Launch readiness" || got.SessionKey != "agent:mission-launch:s1" {
		t.Fatalf("got = %+v, want name/session roundtrip", got)
	}
	if len(got.Goals) != 1 || got.Goals[0] != "Ship v1" {
		t.Fatalf("goals = %v, want [Ship v1]", got.Goals)
	}
	if len(got.Milestones) != 2 || got.Milestones[1] != "Build" {
		t.Fatalf("milestones = %v, want [Design Build]", got.Milestones)
	}
	if len(got.Acceptance) != 1 || got.Acceptance[0] != "All tests green" {
		t.Fatalf("acceptance = %v, want [All tests green]", got.Acceptance)
	}
	if got.AgentID == nil || *got.AgentID != agentID {
		t.Fatalf("agent_id = %v, want %v", got.AgentID, agentID)
	}
	if string(got.Metadata) != `{"channel":"cron"}` {
		t.Fatalf("metadata = %s, want {\"channel\":\"cron\"}", got.Metadata)
	}
}

func TestSQLiteMissionStoreListStatusFilter(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	tenantID := uuid.Must(uuid.NewV7())
	seedSQLiteMissionTenant(t, db, tenantID, uuid.Must(uuid.NewV7()))

	ms := NewSQLiteMissionStore(db)
	ctx := missionCtx(tenantID)

	active := store.Mission{Name: "active one"}
	paused := store.Mission{Name: "paused one", Status: store.MissionStatusPaused}
	if err := ms.CreateMission(ctx, &active); err != nil {
		t.Fatalf("CreateMission active: %v", err)
	}
	if err := ms.CreateMission(ctx, &paused); err != nil {
		t.Fatalf("CreateMission paused: %v", err)
	}

	all, err := ms.ListMissions(ctx, store.MissionListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListMissions(all): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2", len(all))
	}

	pausedList, err := ms.ListMissions(ctx, store.MissionListOpts{Status: store.MissionStatusPaused, Limit: 10})
	if err != nil {
		t.Fatalf("ListMissions(paused): %v", err)
	}
	if len(pausedList) != 1 || pausedList[0].Name != "paused one" {
		t.Fatalf("pausedList = %+v, want [paused one]", pausedList)
	}
}

func TestSQLiteMissionStoreStatusAndProgress(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	tenantID := uuid.Must(uuid.NewV7())
	seedSQLiteMissionTenant(t, db, tenantID, uuid.Must(uuid.NewV7()))

	ms := NewSQLiteMissionStore(db)
	ctx := missionCtx(tenantID)

	m := store.Mission{Name: "progress"}
	if err := ms.CreateMission(ctx, &m); err != nil {
		t.Fatalf("CreateMission: %v", err)
	}

	if err := ms.UpdateMissionStatus(ctx, m.ID, store.MissionStatusPaused); err != nil {
		t.Fatalf("UpdateMissionStatus: %v", err)
	}
	if err := ms.UpdateMissionProgress(ctx, m.ID, 7); err != nil {
		t.Fatalf("UpdateMissionProgress: %v", err)
	}

	got, err := ms.GetMission(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetMission: %v", err)
	}
	if got.Status != store.MissionStatusPaused {
		t.Fatalf("status = %q, want paused", got.Status)
	}
	if got.CheckpointSeq != 7 {
		t.Fatalf("checkpoint_seq = %d, want 7", got.CheckpointSeq)
	}

	if err := ms.UpdateMissionStatus(ctx, m.ID, "bogus"); err == nil {
		t.Fatal("UpdateMissionStatus(bogus) = nil error, want rejection")
	}
	if err := ms.UpdateMissionProgress(ctx, m.ID, -1); err == nil {
		t.Fatal("UpdateMissionProgress(-1) = nil error, want rejection")
	}
}

func TestSQLiteMissionStoreDelete(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	tenantID := uuid.Must(uuid.NewV7())
	seedSQLiteMissionTenant(t, db, tenantID, uuid.Must(uuid.NewV7()))

	ms := NewSQLiteMissionStore(db)
	ctx := missionCtx(tenantID)

	m := store.Mission{Name: "delete me"}
	if err := ms.CreateMission(ctx, &m); err != nil {
		t.Fatalf("CreateMission: %v", err)
	}
	if err := ms.DeleteMission(ctx, m.ID); err != nil {
		t.Fatalf("DeleteMission: %v", err)
	}
	if _, err := ms.GetMission(ctx, m.ID); err == nil {
		t.Fatal("GetMission after delete = nil error, want store.ErrMissionNotFound")
	}
}

func TestSQLiteMissionStoreTenantIsolation(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	tenantA := uuid.Must(uuid.NewV7())
	tenantB := uuid.Must(uuid.NewV7())
	seedSQLiteMissionTenant(t, db, tenantA, uuid.Must(uuid.NewV7()))
	seedSQLiteMissionTenant(t, db, tenantB, uuid.Must(uuid.NewV7()))

	ms := NewSQLiteMissionStore(db)
	ctxA := missionCtx(tenantA)
	ctxB := missionCtx(tenantB)

	mA := store.Mission{Name: "tenant A mission"}
	if err := ms.CreateMission(ctxA, &mA); err != nil {
		t.Fatalf("CreateMission A: %v", err)
	}

	if _, err := ms.GetMission(ctxB, mA.ID); err == nil {
		t.Fatal("tenant B read tenant A mission: want error")
	}

	listB, err := ms.ListMissions(ctxB, store.MissionListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListMissions B: %v", err)
	}
	for _, m := range listB {
		if m.ID == mA.ID {
			t.Fatalf("tenant B saw tenant A mission %s", mA.ID)
		}
	}

	// Cross-tenant writes are scoped by tenant_id and silently no-op.
	mB := store.Mission{Name: "tenant B mission"}
	if err := ms.CreateMission(ctxB, &mB); err != nil {
		t.Fatalf("CreateMission B: %v", err)
	}
	if err := ms.UpdateMissionStatus(ctxB, mA.ID, store.MissionStatusCancelled); err != nil {
		t.Fatalf("UpdateMissionStatus cross-tenant: unexpected error %v", err)
	}
	if got, err := ms.GetMission(ctxA, mA.ID); err != nil || got == nil || got.Status == store.MissionStatusCancelled {
		t.Fatalf("tenant A mission wrong after tenant B status write: got=%+v err=%v", got, err)
	}
	if err := ms.DeleteMission(ctxB, mA.ID); err != nil {
		t.Fatalf("DeleteMission cross-tenant: unexpected error %v", err)
	}
	if got, err := ms.GetMission(ctxA, mA.ID); err != nil || got == nil {
		t.Fatalf("tenant A mission vanished after tenant B delete attempt: got=%v err=%v", got, err)
	}
}

func TestSQLiteMissionStoreRejectsUnknownStatusOnCreate(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	tenantID := uuid.Must(uuid.NewV7())
	seedSQLiteMissionTenant(t, db, tenantID, uuid.Must(uuid.NewV7()))

	ms := NewSQLiteMissionStore(db)
	ctx := missionCtx(tenantID)

	m := store.Mission{Name: "bad status", Status: "bogus"}
	if err := ms.CreateMission(ctx, &m); err == nil {
		t.Fatal("CreateMission(bogus status) = nil error, want rejection")
	}
}

package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// missionMetadataEqual compares two JSONB metadata blobs semantically.
// PostgreSQL stores metadata as JSONB, which normalizes whitespace/key
// formatting on output, so raw string comparison would spuriously fail.
func missionMetadataEqual(t *testing.T, got, want []byte) bool {
	t.Helper()
	var gotJSON, wantJSON any
	if err := json.Unmarshal(got, &gotJSON); err != nil {
		t.Fatalf("parse got metadata: %v", err)
	}
	if err := json.Unmarshal(want, &wantJSON); err != nil {
		t.Fatalf("parse want metadata: %v", err)
	}
	return reflect.DeepEqual(gotJSON, wantJSON)
}

// missionTestDB opens a migrated PG test database. Skips when TEST_DATABASE_URL
// is unset or PG is unreachable (the controller gate runs with pgvector:pg18 on
// port 5433).
func missionTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skipf("TEST_DATABASE_URL not set; skipping PG mission store tests")
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		t.Skipf("PG not reachable: %v", err)
	}

	m, err := migrate.New("file://../../../migrations", dsn)
	if err != nil {
		db.Close()
		t.Fatalf("migrate.New: %v", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		db.Close()
		t.Fatalf("migrate up: %v", err)
	}
	m.Close()

	InitSqlx(db)
	t.Cleanup(func() { db.Close() })
	return db
}

// seedMissionTenant inserts a minimal tenant + agent row and registers cleanup.
func seedMissionTenant(t *testing.T, db *sql.DB) (tenantID, agentID uuid.UUID) {
	t.Helper()
	tenantID = uuid.Must(uuid.NewV7())
	agentID = uuid.Must(uuid.NewV7())

	_, err := db.Exec(
		`INSERT INTO tenants (id, name, slug, status) VALUES ($1,$2,$3,'active') ON CONFLICT DO NOTHING`,
		tenantID, "mission-test-"+tenantID.String()[:8], "mission-"+tenantID.String())
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
	_, err = db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES ($1,$2,$3,'predefined','active','test','test-model','owner') ON CONFLICT DO NOTHING`,
		agentID, tenantID, "mission-agent-"+agentID.String())
	if err != nil {
		t.Fatalf("seed agent: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM missions WHERE tenant_id=$1", tenantID)
		db.Exec("DELETE FROM agents WHERE id=$1", agentID)
		db.Exec("DELETE FROM tenants WHERE id=$1", tenantID)
	})
	return tenantID, agentID
}

func missionCtx(tenantID uuid.UUID) context.Context {
	return store.WithTenantID(context.Background(), tenantID)
}

func TestPGMissionStore_CreateGetRoundtrip(t *testing.T) {
	db := missionTestDB(t)
	tenantID, agentID := seedMissionTenant(t, db)
	s := NewPGMissionStore(db)
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
	if err := s.CreateMission(ctx, &m); err != nil {
		t.Fatalf("CreateMission: %v", err)
	}
	if m.ID == uuid.Nil {
		t.Fatalf("CreateMission did not assign ID")
	}
	if m.Status != store.MissionStatusActive {
		t.Fatalf("status = %q, want default active", m.Status)
	}
	if m.TenantID != tenantID {
		t.Fatalf("tenant_id = %v, want seeded tenant %v", m.TenantID, tenantID)
	}

	got, err := s.GetMission(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetMission: %v", err)
	}
	if got.Name != "Launch readiness" {
		t.Fatalf("name = %q, want Launch readiness", got.Name)
	}
	if got.SessionKey != "agent:mission-launch:s1" {
		t.Fatalf("session_key = %q, want agent:mission-launch:s1", got.SessionKey)
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
	if !missionMetadataEqual(t, got.Metadata, []byte(`{"channel":"cron"}`)) {
		t.Fatalf("metadata = %s, want {\"channel\":\"cron\"}", got.Metadata)
	}
}

func TestPGMissionStore_GetMissing(t *testing.T) {
	db := missionTestDB(t)
	tenantID, _ := seedMissionTenant(t, db)
	s := NewPGMissionStore(db)
	ctx := missionCtx(tenantID)

	if _, err := s.GetMission(ctx, uuid.Must(uuid.NewV7())); !errors.Is(err, store.ErrMissionNotFound) {
		t.Fatalf("GetMission(missing) err = %v, want store.ErrMissionNotFound", err)
	}
}

func TestPGMissionStore_ListStatusFilter(t *testing.T) {
	db := missionTestDB(t)
	tenantID, _ := seedMissionTenant(t, db)
	s := NewPGMissionStore(db)
	ctx := missionCtx(tenantID)

	active := store.Mission{Name: "active one"}
	if err := s.CreateMission(ctx, &active); err != nil {
		t.Fatalf("CreateMission active: %v", err)
	}
	paused := store.Mission{Name: "paused one", Status: store.MissionStatusPaused}
	if err := s.CreateMission(ctx, &paused); err != nil {
		t.Fatalf("CreateMission paused: %v", err)
	}

	all, err := s.ListMissions(ctx, store.MissionListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListMissions(all): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len(all) = %d, want 2", len(all))
	}

	pausedList, err := s.ListMissions(ctx, store.MissionListOpts{Status: store.MissionStatusPaused, Limit: 10})
	if err != nil {
		t.Fatalf("ListMissions(paused): %v", err)
	}
	if len(pausedList) != 1 || pausedList[0].Name != "paused one" {
		t.Fatalf("pausedList = %+v, want [paused one]", pausedList)
	}
}

func TestPGMissionStore_StatusAndProgress(t *testing.T) {
	db := missionTestDB(t)
	tenantID, _ := seedMissionTenant(t, db)
	s := NewPGMissionStore(db)
	ctx := missionCtx(tenantID)

	m := store.Mission{Name: "progress"}
	if err := s.CreateMission(ctx, &m); err != nil {
		t.Fatalf("CreateMission: %v", err)
	}

	if err := s.UpdateMissionStatus(ctx, m.ID, store.MissionStatusPaused); err != nil {
		t.Fatalf("UpdateMissionStatus: %v", err)
	}
	if err := s.UpdateMissionProgress(ctx, m.ID, 7); err != nil {
		t.Fatalf("UpdateMissionProgress: %v", err)
	}

	got, err := s.GetMission(ctx, m.ID)
	if err != nil {
		t.Fatalf("GetMission: %v", err)
	}
	if got.Status != store.MissionStatusPaused {
		t.Fatalf("status = %q, want paused", got.Status)
	}
	if got.CheckpointSeq != 7 {
		t.Fatalf("checkpoint_seq = %d, want 7", got.CheckpointSeq)
	}

	// Unknown status must be rejected.
	if err := s.UpdateMissionStatus(ctx, m.ID, "bogus"); err == nil {
		t.Fatal("UpdateMissionStatus(bogus) = nil error, want rejection")
	}
	// Negative checkpoint seq must be rejected.
	if err := s.UpdateMissionProgress(ctx, m.ID, -1); err == nil {
		t.Fatal("UpdateMissionProgress(-1) = nil error, want rejection")
	}
}

func TestPGMissionStore_Delete(t *testing.T) {
	db := missionTestDB(t)
	tenantID, _ := seedMissionTenant(t, db)
	s := NewPGMissionStore(db)
	ctx := missionCtx(tenantID)

	m := store.Mission{Name: "delete me"}
	if err := s.CreateMission(ctx, &m); err != nil {
		t.Fatalf("CreateMission: %v", err)
	}
	if err := s.DeleteMission(ctx, m.ID); err != nil {
		t.Fatalf("DeleteMission: %v", err)
	}
	if _, err := s.GetMission(ctx, m.ID); !errors.Is(err, store.ErrMissionNotFound) {
		t.Fatalf("GetMission after delete = %v, want store.ErrMissionNotFound", err)
	}
}

func TestPGMissionStore_TenantIsolation(t *testing.T) {
	db := missionTestDB(t)
	tenantA, _ := seedMissionTenant(t, db)
	tenantB, _ := seedMissionTenant(t, db)
	s := NewPGMissionStore(db)

	ctxA := missionCtx(tenantA)
	ctxB := missionCtx(tenantB)

	mA := store.Mission{Name: "tenant A mission"}
	if err := s.CreateMission(ctxA, &mA); err != nil {
		t.Fatalf("CreateMission A: %v", err)
	}

	// Tenant B cannot read tenant A's mission.
	if _, err := s.GetMission(ctxB, mA.ID); err == nil {
		t.Fatal("tenant B read tenant A mission: want error")
	}

	// Tenant B's list does not include tenant A's mission.
	listB, err := s.ListMissions(ctxB, store.MissionListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListMissions B: %v", err)
	}
	for _, m := range listB {
		if m.ID == mA.ID {
			t.Fatalf("tenant B saw tenant A mission %s", mA.ID)
		}
	}

	// Tenant B cannot update or delete tenant A's mission (no rows affected —
	// the UPDATE/DELETE is scoped by tenant_id, so it silently no-ops).
	mB := store.Mission{Name: "tenant B mission"}
	if err := s.CreateMission(ctxB, &mB); err != nil {
		t.Fatalf("CreateMission B: %v", err)
	}
	if err := s.UpdateMissionStatus(ctxB, mA.ID, store.MissionStatusCancelled); err != nil {
		t.Fatalf("UpdateMissionStatus cross-tenant: unexpected error %v", err)
	}
	if got, _ := s.GetMission(ctxA, mA.ID); got != nil && got.Status == store.MissionStatusCancelled {
		t.Fatalf("tenant B changed tenant A mission status to %s", got.Status)
	}
	if err := s.DeleteMission(ctxB, mA.ID); err != nil {
		t.Fatalf("DeleteMission cross-tenant: unexpected error %v", err)
	}
	if got, err := s.GetMission(ctxA, mA.ID); err != nil || got == nil {
		t.Fatalf("tenant A mission vanished after tenant B delete attempt: got=%v err=%v", got, err)
	}
}

func TestPGMissionStore_RejectsUnknownStatusOnCreate(t *testing.T) {
	db := missionTestDB(t)
	tenantID, _ := seedMissionTenant(t, db)
	s := NewPGMissionStore(db)
	ctx := missionCtx(tenantID)

	m := store.Mission{Name: "bad status", Status: "bogus"}
	if err := s.CreateMission(ctx, &m); err == nil {
		t.Fatal("CreateMission(bogus status) = nil error, want rejection")
	}
}

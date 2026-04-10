//go:build integration

package integration

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

func TestStoreTeam_CreateAndGet(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ts := pg.NewPGTeamStore(db)

	team := &store.TeamData{
		Name:        "test-team-" + uuid.New().String()[:8],
		LeadAgentID: agentID,
		Status:      store.TeamStatusActive,
		CreatedBy:   "test",
	}
	if err := ts.CreateTeam(ctx, team); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	if team.ID == uuid.Nil {
		t.Fatal("CreateTeam did not assign ID")
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_team_members WHERE team_id = $1", team.ID)
		db.Exec("DELETE FROM agent_teams WHERE id = $1", team.ID)
	})

	got, err := ts.GetTeam(ctx, team.ID)
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if got == nil {
		t.Fatal("GetTeam returned nil")
	}
	if got.Name != team.Name {
		t.Errorf("Name: expected %q, got %q", team.Name, got.Name)
	}
	if got.LeadAgentID != agentID {
		t.Errorf("LeadAgentID mismatch: expected %v, got %v", agentID, got.LeadAgentID)
	}
	if got.Status != store.TeamStatusActive {
		t.Errorf("Status: expected %q, got %q", store.TeamStatusActive, got.Status)
	}
}

func TestStoreTeam_Members(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ts := pg.NewPGTeamStore(db)

	agent2ID := uuid.New()
	if _, err := db.Exec(
		`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		 VALUES ($1, $2, $3, 'predefined', 'active', 'test', 'test-model', 'test-owner')`,
		agent2ID, tenantID, "member2-"+agent2ID.String()[:8],
	); err != nil {
		t.Fatalf("seed agent2: %v", err)
	}

	team := &store.TeamData{
		Name:        "members-test-" + uuid.New().String()[:8],
		LeadAgentID: agentID,
		Status:      store.TeamStatusActive,
		CreatedBy:   "test",
	}
	if err := ts.CreateTeam(ctx, team); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_team_members WHERE team_id = $1", team.ID)
		db.Exec("DELETE FROM agent_teams WHERE id = $1", team.ID)
		db.Exec("DELETE FROM agents WHERE id = $1", agent2ID)
	})

	if err := ts.AddMember(ctx, team.ID, agentID, store.TeamRoleLead); err != nil {
		t.Fatalf("AddMember lead: %v", err)
	}
	if err := ts.AddMember(ctx, team.ID, agent2ID, store.TeamRoleMember); err != nil {
		t.Fatalf("AddMember member: %v", err)
	}

	members, err := ts.ListMembers(ctx, team.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("ListMembers: expected 2, got %d", len(members))
	}

	if err := ts.RemoveMember(ctx, team.ID, agent2ID); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}

	members2, err := ts.ListMembers(ctx, team.ID)
	if err != nil {
		t.Fatalf("ListMembers after remove: %v", err)
	}
	if len(members2) != 1 {
		t.Errorf("after remove: expected 1, got %d", len(members2))
	}
	if members2[0].AgentID != agentID {
		t.Errorf("remaining member: expected %v, got %v", agentID, members2[0].AgentID)
	}
}

func TestStoreTeam_GetTeamForAgent(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantID, agentID := seedTenantAgent(t, db)
	ctx := tenantCtx(tenantID)
	ts := pg.NewPGTeamStore(db)

	teamID, _ := seedTeam(t, db, tenantID, agentID)

	got, err := ts.GetTeamForAgent(ctx, agentID)
	if err != nil {
		t.Fatalf("GetTeamForAgent: %v", err)
	}
	if got == nil {
		t.Fatal("GetTeamForAgent returned nil")
	}
	if got.ID != teamID {
		t.Errorf("team ID: expected %v, got %v", teamID, got.ID)
	}
}

func TestStoreTeam_TenantIsolation(t *testing.T) {
	db := testDB(t)
	pg.InitSqlx(db)
	tenantA, tenantB, agentA, _ := seedTwoTenants(t, db)
	ctxA := tenantCtx(tenantA)
	ctxB := tenantCtx(tenantB)
	ts := pg.NewPGTeamStore(db)

	team := &store.TeamData{
		Name:        "isolation-" + uuid.New().String()[:8],
		LeadAgentID: agentA,
		Status:      store.TeamStatusActive,
		CreatedBy:   "test",
	}
	if err := ts.CreateTeam(ctxA, team); err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_team_members WHERE team_id = $1", team.ID)
		db.Exec("DELETE FROM agent_teams WHERE id = $1", team.ID)
	})

	got, err := ts.GetTeam(ctxB, team.ID)
	// Acceptable outcomes: (nil, nil) or (nil, sql.ErrNoRows) — both mean not found.
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetTeam from tenantB unexpected error: %v", err)
	}
	if got != nil {
		t.Error("tenant isolation broken: tenantB can see tenantA team")
	}
}

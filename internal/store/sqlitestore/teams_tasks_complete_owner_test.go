//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// seedTeamWithTwoAgents creates a tenant, two agents and a team for task tests.
func seedTeamWithTwoAgents(t *testing.T) (*SQLiteTeamStore, context.Context, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	db := newHookTestDB(t)
	tenantID, agentA := seedHookTenantAgent(t, db)
	agentB := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(`INSERT INTO agents (id, tenant_id, agent_key, agent_type, status, provider, model, owner_id)
		VALUES (?,?,?,'predefined','active','test','test-model','owner')`,
		agentB.String(), tenantID.String(), "hb-"+agentB.String()[:8]); err != nil {
		t.Fatalf("seed agent B: %v", err)
	}
	teamID := uuid.Must(uuid.NewV7())
	if _, err := db.Exec(`INSERT INTO agent_teams (id, name, lead_agent_id, created_by, tenant_id) VALUES (?,?,?,?,?)`,
		teamID.String(), "team", agentA.String(), "user", tenantID.String()); err != nil {
		t.Fatalf("seed team: %v", err)
	}
	return NewSQLiteTeamStore(db), sqliteTenantCtx(tenantID), teamID, agentA, agentB
}

// Regression for #1133: CompleteTask must not let a teammate complete (and
// overwrite the result of) a task claimed by another agent.
func TestSQLiteCompleteTask_NonOwnerRejected(t *testing.T) {
	ts, ctx, teamID, agentA, agentB := seedTeamWithTwoAgents(t)
	task := &store.TeamTaskData{TeamID: teamID, Subject: "B's task", Status: store.TeamTaskStatusPending, Metadata: map[string]any{"k": "v"}}
	if err := ts.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := ts.ClaimTask(ctx, task.ID, agentB, teamID); err != nil {
		t.Fatalf("ClaimTask B: %v", err)
	}

	// Same sequence as team_tasks(action=complete) issued by agent A.
	_ = ts.ClaimTask(ctx, task.ID, agentA, teamID)
	if err := ts.CompleteTask(ctx, task.ID, teamID, agentA, "overwritten by A"); err == nil {
		t.Fatal("expected non-owner CompleteTask to fail")
	}

	got, err := ts.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if got.Status != store.TeamTaskStatusInProgress {
		t.Errorf("status = %q, want %q", got.Status, store.TeamTaskStatusInProgress)
	}
	if got.Result != nil {
		t.Errorf("result overwritten by non-owner: %q", *got.Result)
	}
	if got.OwnerAgentID == nil || *got.OwnerAgentID != agentB {
		t.Errorf("owner = %v, want %v", got.OwnerAgentID, agentB)
	}

	// Owner can still complete.
	if err := ts.CompleteTask(ctx, task.ID, teamID, agentB, "done by B"); err != nil {
		t.Fatalf("owner CompleteTask: %v", err)
	}
	got, _ = ts.GetTask(ctx, task.ID)
	if got.Status != store.TeamTaskStatusCompleted || got.Result == nil || *got.Result != "done by B" {
		t.Errorf("after owner complete: status=%q result=%v", got.Status, got.Result)
	}
}

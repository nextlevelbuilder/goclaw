package pg

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkclassify"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func TestPGGetTeamForAgentBuildsCanonicalCoordinator(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, leadID := seedTenantAndAgent(t, db)
	teamID := uuid.New()
	leadKey := "hook-agent-" + leadID.String()
	if _, err := db.Exec(`UPDATE agents SET display_name='Canonical Lead' WHERE id=$1`, leadID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agent_teams (id,name,lead_agent_id,status,settings,created_by,tenant_id)
		VALUES ($1,'Runtime Team',$2,'active','{}','owner',$3)`, teamID, leadID, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agent_team_members (team_id,agent_id,role,tenant_id) VALUES ($1,$2,'lead',$3)`, teamID, leadID, tenantID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Exec(`DELETE FROM agent_teams WHERE id=$1`, teamID) })

	ctx := store.WithTenantID(context.Background(), tenantID)
	teamStore := NewPGTeamStore(db)
	team, err := teamStore.GetTeamForAgent(ctx, leadID)
	if err != nil {
		t.Fatal(err)
	}
	if team == nil || team.LeadAgentID != leadID || team.LeadAgentKey != leadKey || team.LeadDisplayName != "Canonical Lead" {
		t.Fatalf("canonical team = %+v", team)
	}
	input := teamworkclassify.BuildInputFromStores(ctx, teamworkclassify.ProfileStores{
		Agents: NewPGAgentStore(db), Teams: teamStore,
	}, teamworkclassify.BuildInputOptions{Mode: teamworkclassify.ModeTeam, AgentID: leadID, Message: "coordinate staged work"})
	if input.CoordinatorAgentID != leadID || input.CoordinatorAgentKey != leadKey || input.TeamRole != "lead" {
		t.Fatalf("runtime classifier input coordinator=%s/%q role=%q", input.CoordinatorAgentID, input.CoordinatorAgentKey, input.TeamRole)
	}
}

func TestPGTaskEventIdentityRejectsCrossTenantReplay(t *testing.T) {
	db := hooksTestDB(t)
	tenantA, agentA := seedTenantAndAgent(t, db)
	tenantB, agentB := seedTenantAndAgent(t, db)
	teamA, teamB := uuid.New(), uuid.New()
	for _, fixture := range []struct {
		teamID, leadID, tenantID uuid.UUID
		name                     string
	}{
		{teamID: teamA, leadID: agentA, tenantID: tenantA, name: "Event Team A"},
		{teamID: teamB, leadID: agentB, tenantID: tenantB, name: "Event Team B"},
	} {
		if _, err := db.Exec(`INSERT INTO agent_teams (id,name,lead_agent_id,status,settings,created_by,tenant_id)
			VALUES ($1,$2,$3,'active','{"version":2}','owner',$4)`, fixture.teamID, fixture.name, fixture.leadID, fixture.tenantID); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_teams WHERE id IN ($1,$2)", teamA, teamB)
	})

	teamStore := NewPGTeamStore(db)
	taskA := &store.TeamTaskData{TeamID: teamA, Subject: "Task A", Status: store.TeamTaskStatusPending, OwnerAgentID: &agentA, Metadata: map[string]any{}}
	taskB := &store.TeamTaskData{TeamID: teamB, Subject: "Task B", Status: store.TeamTaskStatusPending, OwnerAgentID: &agentB, Metadata: map[string]any{}}
	if err := teamStore.CreateTask(store.WithTenantID(context.Background(), tenantA), taskA); err != nil {
		t.Fatal(err)
	}
	if err := teamStore.CreateTask(store.WithTenantID(context.Background(), tenantB), taskB); err != nil {
		t.Fatal(err)
	}

	eventID := uuid.New()
	claim := func(tenantID, taskID uuid.UUID, eventType string) store.TaskEventClaimResult {
		t.Helper()
		result, err := teamStore.ClaimTaskEvent(store.WithTenantID(context.Background(), tenantID), &store.TeamTaskEventData{
			ID: eventID, TaskID: taskID, EventType: eventType, ActorType: "system",
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	if got := claim(tenantA, taskA.ID, "progress"); got != store.TaskEventClaimed {
		t.Fatalf("first claim = %q", got)
	}
	if got := claim(tenantA, taskA.ID, "progress"); got != store.TaskEventDuplicate {
		t.Fatalf("exact replay = %q", got)
	}
	if got := claim(tenantB, taskB.ID, "progress"); got != store.TaskEventConflict {
		t.Fatalf("cross-tenant replay = %q", got)
	}
	if got := claim(tenantA, taskA.ID, "commented"); got != store.TaskEventConflict {
		t.Fatalf("same-tenant type collision = %q", got)
	}
}

func TestPGWorkflowDispatchLimitSettlesWorkflowFailure(t *testing.T) {
	db := hooksTestDB(t)
	tenantID, leadID := seedTenantAndAgent(t, db)
	workerID, teamID := uuid.New(), uuid.New()
	if _, err := db.Exec(`INSERT INTO agents (id,tenant_id,agent_key,agent_type,status,provider,model,owner_id)
		VALUES ($1,$2,$3,'predefined','active','test','test-model','owner')`, workerID, tenantID, "workflow-worker-"+workerID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agent_teams (id,name,lead_agent_id,status,settings,created_by,tenant_id)
		VALUES ($1,$2,$3,'active','{"version":2}','owner',$4)`, teamID, "Workflow Team", leadID, tenantID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM agent_teams WHERE id=$1", teamID)
		db.Exec("DELETE FROM agents WHERE id=$1", workerID)
	})

	ctx := store.WithTenantID(context.Background(), tenantID)
	teamStore := NewPGTeamStore(db)
	plan := []byte(`{"schema_version":1,"goal":"dispatch failure","steps":[]}`)
	workflow := &store.TeamWorkflowData{
		TeamID: teamID, Status: store.TeamWorkflowStatusRunning, CanonicalPlan: plan, SchemaVersion: 1,
		PlanHash: fmt.Sprintf("%x", sha256.Sum256(plan)), CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead",
		OriginAgentID: leadID, OriginAgentKey: "lead", OriginRunID: "dispatch-limit-run",
		OriginSessionKey: "agent:lead:ws:direct:user", OriginChannel: "ws", OriginChatID: "user",
	}
	taskID := uuid.New()
	tasks := []store.TeamTaskData{{
		BaseModel: store.BaseModel{ID: taskID}, TeamID: teamID, Subject: "Exhausted step",
		Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "only",
		WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true,
		Metadata: map[string]any{"dispatch_count": float64(2)},
	}}
	if err := teamStore.CreateWorkflowWithTasks(ctx, workflow, tasks); err != nil {
		t.Fatal(err)
	}
	if _, err := teamStore.ClaimWorkflowTaskDispatch(ctx, taskID, teamID, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	task, err := teamStore.GetTask(ctx, taskID)
	if err != nil {
		t.Fatal(err)
	}
	if count, ok := task.Metadata["dispatch_count"].(float64); !ok || int(count) != 3 {
		t.Fatalf("durable dispatch_count=%v, want 3", task.Metadata["dispatch_count"])
	}
	if _, err := teamStore.RequeueExpiredWorkflowDispatches(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	manager := tools.NewTeamToolManager(teamStore, nil, bus.New(), t.TempDir())
	manager.DispatchUnblockedTasks(ctx, teamID)

	failedTask, err := teamStore.GetTask(ctx, taskID)
	if err != nil || failedTask.Status != store.TeamTaskStatusFailed {
		t.Fatalf("task status=%v err=%v", failedTask.Status, err)
	}
	failedWorkflow, err := teamStore.GetWorkflow(ctx, workflow.ID)
	if err != nil || failedWorkflow.Status != store.TeamWorkflowStatusFailing {
		t.Fatalf("workflow status=%v err=%v", failedWorkflow.Status, err)
	}
}

func TestPGTaskStoresRejectCanonicalOwnerMismatchBeforeMutation(t *testing.T) {
	teamStore := NewPGTeamStore(nil)
	expectedOwner, attemptedOwner := uuid.New(), uuid.New()
	ctx := store.WithExpectedTaskOwnerID(context.Background(), expectedOwner)
	if err := teamStore.CreateTask(ctx, &store.TeamTaskData{OwnerAgentID: &attemptedOwner}); err == nil {
		t.Fatal("CreateTask accepted an owner that differs from the canonical store constraint")
	}
	workflow := &store.TeamWorkflowData{CoordinatorAgentID: expectedOwner}
	if err := teamStore.CreatePendingWorkflowRequest(context.Background(), workflow, &store.TeamTaskData{OwnerAgentID: &attemptedOwner}); err == nil {
		t.Fatal("CreatePendingWorkflowRequest accepted an audit owner that differs from coordinator")
	}
}

// TestPGTaskReadPopulatesEventBuilderFields is the PG twin of Item 1 case (7):
// a task read must carry the owner agent_key (resolved via the agents JOIN,
// never a UUID), workflow ID, workflow step ID, and plan revision — the exact
// fields BuildTeamTaskEventPayload derives from the committed row. The
// authoritative event payload is only correct if the read populates these.
func TestPGTaskReadPopulatesEventBuilderFields(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
	wantOwnerKey := "recovery-worker-" + workerID.String()
	rootID := uuid.New()
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: rootID}, TeamID: teamID, Subject: "Root", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "root", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})

	task, err := teamStore.GetTask(ctx, rootID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.OwnerAgentKey != wantOwnerKey {
		t.Fatalf("owner agent_key = %q, want %q (agents JOIN)", task.OwnerAgentKey, wantOwnerKey)
	}
	if task.WorkflowID == nil || *task.WorkflowID != workflow.ID {
		t.Fatalf("workflow ID = %v, want %s", task.WorkflowID, workflow.ID)
	}
	if task.WorkflowStepID != "root" || task.PlanRevision != 1 {
		t.Fatalf("workflow step/revision = %q/%d, want root/1", task.WorkflowStepID, task.PlanRevision)
	}

	payload := tools.BuildTeamTaskEventPayload(task, "Worker Display", tools.TeamTaskEventOptions{ActorType: "system"})
	if payload.OwnerAgentKey != wantOwnerKey || payload.OwnerDisplayName != "Worker Display" {
		t.Fatalf("payload owner = %q/%q, want %q/Worker Display", payload.OwnerAgentKey, payload.OwnerDisplayName, wantOwnerKey)
	}
	if payload.WorkflowID != workflow.ID.String() || payload.WorkflowStepID != "root" || payload.PlanRevision != 1 {
		t.Fatalf("payload workflow fields = %q/%q/%d", payload.WorkflowID, payload.WorkflowStepID, payload.PlanRevision)
	}
	if payload.TeamID != teamID.String() || payload.TaskID != rootID.String() || payload.Status != store.TeamTaskStatusPending {
		t.Fatalf("payload identity = %+v", payload)
	}
}

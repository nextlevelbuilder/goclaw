//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/bus"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/teamworkclassify"
	"github.com/nextlevelbuilder/goclaw/internal/tools"
)

func TestSQLiteGetTeamForAgentBuildsCanonicalCoordinator(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	tenantID, leadID, teamID := uuid.New(), uuid.New(), uuid.New()
	if _, err := db.Exec(`INSERT INTO tenants(id,name,slug,status,settings) VALUES(?,?,?,'active','{}')`, tenantID, "Tenant", "tenant-"+tenantID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agents(id,agent_key,display_name,owner_id,provider,model,tenant_id) VALUES(?,?,?,'owner','openai','test',?)`, leadID, "canonical-lead", "Canonical Lead", tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agent_teams(id,name,lead_agent_id,status,settings,created_by,tenant_id) VALUES(?,?,?,'active','{}','owner',?)`, teamID, "Runtime Team", leadID, tenantID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agent_team_members(team_id,agent_id,role,tenant_id) VALUES(?,?,'lead',?)`, teamID, leadID, tenantID); err != nil {
		t.Fatal(err)
	}

	ctx := store.WithTenantID(context.Background(), tenantID)
	teamStore := NewSQLiteTeamStore(db)
	team, err := teamStore.GetTeamForAgent(ctx, leadID)
	if err != nil {
		t.Fatal(err)
	}
	if team == nil || team.LeadAgentID != leadID || team.LeadAgentKey != "canonical-lead" || team.LeadDisplayName != "Canonical Lead" {
		t.Fatalf("canonical team = %+v", team)
	}
	input := teamworkclassify.BuildInputFromStores(ctx, teamworkclassify.ProfileStores{
		Agents: NewSQLiteAgentStore(db), Teams: teamStore,
	}, teamworkclassify.BuildInputOptions{Mode: teamworkclassify.ModeTeam, AgentID: leadID, Message: "coordinate staged work"})
	if input.CoordinatorAgentID != leadID || input.CoordinatorAgentKey != "canonical-lead" || input.TeamRole != "lead" {
		t.Fatalf("runtime classifier input coordinator=%s/%q role=%q", input.CoordinatorAgentID, input.CoordinatorAgentKey, input.TeamRole)
	}
}

func TestSQLiteTaskEventIdentityRejectsCrossTenantReplay(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	tenantA, tenantB := uuid.New(), uuid.New()
	agentA, agentB := uuid.New(), uuid.New()
	teamA, teamB := uuid.New(), uuid.New()
	for _, fixture := range []struct {
		tenantID, agentID, teamID uuid.UUID
		key                       string
	}{
		{tenantID: tenantA, agentID: agentA, teamID: teamA, key: "event-agent-a"},
		{tenantID: tenantB, agentID: agentB, teamID: teamB, key: "event-agent-b"},
	} {
		if _, err := db.Exec(`INSERT INTO tenants(id,name,slug,status,settings) VALUES(?,?,?,'active','{}')`, fixture.tenantID, fixture.key, fixture.key); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO agents(id,agent_key,owner_id,provider,model,tenant_id) VALUES(?,?,?,'openai','test',?)`, fixture.agentID, fixture.key, "owner", fixture.tenantID); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO agent_teams(id,name,lead_agent_id,status,settings,created_by,tenant_id) VALUES(?,?,?,'active','{"version":2}','owner',?)`, fixture.teamID, fixture.key, fixture.agentID, fixture.tenantID); err != nil {
			t.Fatal(err)
		}
	}

	teamStore := NewSQLiteTeamStore(db)
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

func TestSQLiteWorkflowDispatchLimitSettlesWorkflowFailure(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	tenantID, leadID, workerID, teamID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := db.Exec(`INSERT INTO tenants(id,name,slug,status,settings) VALUES(?,?,?,'active','{}')`, tenantID, "Tenant", "tenant-"+tenantID.String()); err != nil {
		t.Fatal(err)
	}
	for id, key := range map[uuid.UUID]string{leadID: "lead", workerID: "worker"} {
		if _, err := db.Exec(`INSERT INTO agents(id,agent_key,owner_id,provider,model,tenant_id) VALUES(?,?,?,'openai','test',?)`, id, key, "owner", tenantID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO agent_teams(id,name,lead_agent_id,status,settings,created_by,tenant_id) VALUES(?,?,?,'active',?,'owner',?)`, teamID, "Team", leadID, []byte(`{"version":2}`), tenantID); err != nil {
		t.Fatal(err)
	}
	ctx := store.WithTenantID(context.Background(), tenantID)
	teamStore := NewSQLiteTeamStore(db)
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
		Metadata: map[string]any{"dispatch_count": float64(maxTaskDispatchesForStoreTest - 1)},
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
	if count, ok := task.Metadata["dispatch_count"].(float64); !ok || int(count) != maxTaskDispatchesForStoreTest {
		t.Fatalf("durable dispatch_count=%v, want %d", task.Metadata["dispatch_count"], maxTaskDispatchesForStoreTest)
	}
	if _, err := teamStore.RequeueExpiredWorkflowDispatches(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	recoverable, err := teamStore.ListRecoverableTasks(ctx, teamID)
	if err != nil || len(recoverable) != 1 {
		t.Fatalf("recoverable tasks=%d err=%v", len(recoverable), err)
	}
	if _, err := teamStore.GetTeam(ctx, teamID); err != nil {
		t.Fatalf("get team: %v", err)
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

func TestSQLiteTaskStoresRejectCanonicalOwnerMismatchBeforeMutation(t *testing.T) {
	teamStore := NewSQLiteTeamStore(nil)
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

const maxTaskDispatchesForStoreTest = 3

func TestSQLiteWorkflowDurabilityAndDispatchCAS(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantID, otherTenantID := uuid.New(), uuid.New()
	leadID, workerID := uuid.New(), uuid.New()
	teamID := uuid.New()
	for _, tenant := range []uuid.UUID{tenantID, otherTenantID} {
		if _, err := db.Exec(`INSERT INTO tenants(id,name,slug,status,settings) VALUES(?,?,?,'active','{}')`, tenant, "Tenant", "tenant-"+tenant.String()); err != nil {
			t.Fatal(err)
		}
	}
	for id, key := range map[uuid.UUID]string{leadID: "lead", workerID: "worker"} {
		if _, err := db.Exec(`INSERT INTO agents(id,agent_key,owner_id,provider,model,tenant_id) VALUES(?,?,?,'openai','test',?)`, id, key, "owner", tenantID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO agent_teams(id,name,lead_agent_id,status,settings,created_by,tenant_id) VALUES(?,?,?,'active','{"version":2}','owner',?)`, teamID, "Team", leadID, tenantID); err != nil {
		t.Fatal(err)
	}
	for id, role := range map[uuid.UUID]string{leadID: "lead", workerID: "member"} {
		if _, err := db.Exec(`INSERT INTO agent_team_members(team_id,agent_id,role,tenant_id) VALUES(?,?,?,?)`, teamID, id, role, tenantID); err != nil {
			t.Fatal(err)
		}
	}
	ctx := store.WithTenantID(context.Background(), tenantID)
	teamStore := NewSQLiteTeamStore(db)
	plan := []byte(`{"schema_version":1,"goal":"test","steps":[]}`)
	hash := fmt.Sprintf("%x", sha256.Sum256(plan))
	workflow := &store.TeamWorkflowData{
		TeamID: teamID, Status: store.TeamWorkflowStatusRunning, CanonicalPlan: plan, SchemaVersion: 1, PlanHash: hash,
		CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead", OriginAgentID: leadID, OriginAgentKey: "lead",
		OriginRunID: "run-1", OriginSessionKey: "agent:lead:ws:direct:user", OriginChannel: "ws", OriginChatID: "user",
	}
	rootID, terminalID := uuid.New(), uuid.New()
	tasks := []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: rootID}, TeamID: teamID, Subject: "Root", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "root", WorkflowKind: store.TeamWorkflowTaskKindWork},
		{BaseModel: store.BaseModel{ID: terminalID}, TeamID: teamID, Subject: "Terminal", Status: store.TeamTaskStatusBlocked, OwnerAgentID: &leadID, BlockedBy: []uuid.UUID{rootID}, WorkflowStepID: "terminal", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	}
	if err := teamStore.CreateWorkflowWithTasks(ctx, workflow, tasks); err != nil {
		t.Fatalf("CreateWorkflowWithTasks: %v", err)
	}
	if workflow.ID == uuid.Nil {
		t.Fatal("workflow ID not assigned")
	}
	duplicate := *workflow
	duplicate.ID = uuid.Nil
	if err := teamStore.CreateWorkflowWithTasks(ctx, &duplicate, tasks); err == nil {
		t.Fatal("duplicate creation key must fail")
	}
	if _, err := teamStore.GetWorkflow(store.WithTenantID(context.Background(), otherTenantID), workflow.ID); err == nil {
		t.Fatal("cross-tenant workflow lookup must fail")
	}
	token, err := teamStore.ClaimWorkflowTaskDispatch(ctx, rootID, teamID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimWorkflowTaskDispatch: %v", err)
	}
	if _, err := teamStore.ClaimWorkflowTaskDispatch(ctx, rootID, teamID, time.Now().Add(time.Minute)); err == nil {
		t.Fatal("second dispatch claim must fail")
	}
	if err := teamStore.AcceptWorkflowTaskDispatch(ctx, rootID, teamID, uuid.New(), time.Now().Add(time.Hour)); err == nil {
		t.Fatal("stale token must fail")
	}
	if err := teamStore.AcceptWorkflowTaskDispatch(ctx, rootID, teamID, token, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("accept dispatch: %v", err)
	}
	if err := teamStore.AcceptWorkflowTaskDispatch(ctx, rootID, teamID, token, time.Now().Add(time.Hour)); err == nil {
		t.Fatal("duplicate consumer accept must fail")
	}
	settlement, err := teamStore.SettleWorkflowTask(ctx, rootID, teamID, "root result", false, time.Time{})
	if err != nil {
		t.Fatalf("settle root: %v", err)
	}
	if settlement.ReadyToFinalize {
		t.Fatal("workflow finalized before terminal")
	}
	terminal, err := teamStore.GetTask(ctx, terminalID)
	if err != nil || terminal.Status != store.TeamTaskStatusPending {
		t.Fatalf("terminal status=%v err=%v, want pending", terminal.Status, err)
	}

	pendingWorkflow := &store.TeamWorkflowData{
		TeamID: teamID, Status: store.TeamWorkflowStatusPendingExpansion, CanonicalPlan: plan, SchemaVersion: 1, PlanHash: hash,
		CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead", OriginAgentID: workerID, OriginAgentKey: "worker",
		OriginRunID: "run-2", OriginSessionKey: "agent:worker:ws:direct:user", OriginChannel: "ws", OriginChatID: "user",
	}
	audit := &store.TeamTaskData{TeamID: teamID, Subject: "Workflow request", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, TaskType: "request", CreatedByAgentID: &workerID}
	if err := teamStore.CreatePendingWorkflowRequest(ctx, pendingWorkflow, audit); err != nil {
		t.Fatalf("CreatePendingWorkflowRequest: %v", err)
	}
	expandedTaskID := uuid.New()
	expandedTasks := []store.TeamTaskData{{BaseModel: store.BaseModel{ID: expandedTaskID}, TeamID: teamID, Subject: "Expanded", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "only", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true}}
	if err := teamStore.ApprovePendingWorkflowRequest(ctx, pendingWorkflow.ID, audit.ID, store.WorkflowApprovalActor{AgentID: &workerID}, expandedTasks); err == nil {
		t.Fatal("non-coordinator approval must fail")
	}
	if err := teamStore.ApprovePendingWorkflowRequest(ctx, pendingWorkflow.ID, audit.ID, store.WorkflowApprovalActor{AgentID: &leadID}, expandedTasks); err != nil {
		t.Fatalf("ApprovePendingWorkflowRequest: %v", err)
	}
	approvedWorkflow, err := teamStore.GetWorkflow(ctx, pendingWorkflow.ID)
	if err != nil || approvedWorkflow.Status != store.TeamWorkflowStatusRunning {
		t.Fatalf("approved workflow=%+v err=%v", approvedWorkflow, err)
	}
	approvedAudit, err := teamStore.GetTask(ctx, audit.ID)
	if err != nil || approvedAudit.Status != store.TeamTaskStatusCompleted {
		t.Fatalf("approved audit=%+v err=%v", approvedAudit, err)
	}
	if err := teamStore.AssignTask(ctx, expandedTaskID, workerID, teamID); err == nil {
		t.Fatal("generic assignment must reject workflow work tasks")
	}
	if err := teamStore.UpdateTask(ctx, expandedTaskID, map[string]any{"subject": "mutated"}); err == nil {
		t.Fatal("generic update must reject workflow work tasks")
	}
	expandedToken, err := teamStore.ClaimWorkflowTaskDispatch(ctx, expandedTaskID, teamID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("claim expanded dispatch: %v", err)
	}
	if err := teamStore.AcceptWorkflowTaskDispatch(ctx, expandedTaskID, teamID, expandedToken, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("accept expanded dispatch: %v", err)
	}
	settlement, err = teamStore.SettleWorkflowTask(ctx, expandedTaskID, teamID, "final result", false, time.Time{})
	if err != nil || !settlement.ReadyToFinalize {
		t.Fatalf("expanded settlement=%+v err=%v", settlement, err)
	}
	_, finalizeToken, err := teamStore.ClaimWorkflowFinalization(ctx, pendingWorkflow.ID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("claim finalization: %v", err)
	}
	if err := teamStore.CompleteWorkflowFinalization(ctx, pendingWorkflow.ID, finalizeToken, store.TeamWorkflowStatusCompleted, "final result"); err != nil {
		t.Fatalf("complete finalization: %v", err)
	}
	claimed, deliveryToken, err := teamStore.ClaimWorkflowDelivery(ctx, pendingWorkflow.ID, time.Now().Add(time.Minute))
	if err != nil || claimed.ResultSummary != "final result" {
		t.Fatalf("delivery claim=%+v err=%v", claimed, err)
	}
	if _, err := teamStore.FailWorkflowDeliveryAttempt(ctx, pendingWorkflow.ID, deliveryToken, "transient send error"); err != nil {
		t.Fatalf("release failed delivery: %v", err)
	}
	if _, _, err := teamStore.ClaimWorkflowDelivery(ctx, pendingWorkflow.ID, time.Now().Add(time.Minute)); err == nil {
		t.Fatal("delivery retry must honor next_delivery_at backoff")
	}
	if _, err := teamStore.db.Exec(`UPDATE team_workflows SET next_delivery_at=? WHERE id=?`, time.Now().Add(-time.Second), pendingWorkflow.ID); err != nil {
		t.Fatalf("make delivery retry due: %v", err)
	}
	claimed, deliveryToken, err = teamStore.ClaimWorkflowDelivery(ctx, pendingWorkflow.ID, time.Now().Add(time.Minute))
	if err != nil || claimed.ResultSummary != "final result" {
		t.Fatalf("delivery retry claim=%+v err=%v", claimed, err)
	}
	if err := teamStore.CompleteWorkflowDelivery(ctx, pendingWorkflow.ID, deliveryToken); err != nil {
		t.Fatalf("complete delivery: %v", err)
	}
	if _, _, err := teamStore.ClaimWorkflowDelivery(ctx, pendingWorkflow.ID, time.Now().Add(time.Minute)); err == nil {
		t.Fatal("delivered workflow must not be claimed again")
	}
	matches, err := teamStore.SearchWorkflows(ctx, teamID, "test", 10)
	if err != nil || len(matches) < 2 {
		t.Fatalf("workflow search matches=%d err=%v", len(matches), err)
	}
}

// sqliteRecoveryFixture builds a tenant/lead/worker/team with a running
// workflow and returns the store + tenant-scoped ctx so the recovery tests can
// drive the real attempt-fenced paths (claim → accept → block/complete) instead
// of poking raw rows.
func sqliteRecoveryStrPtr(s string) *string { return &s }

func sqliteRecoveryFixture(t *testing.T) (*SQLiteTeamStore, context.Context, uuid.UUID, uuid.UUID, uuid.UUID) {
	t.Helper()
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	tenantID, leadID, workerID, teamID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := db.Exec(`INSERT INTO tenants(id,name,slug,status,settings) VALUES(?,?,?,'active','{}')`, tenantID, "Tenant", "tenant-"+tenantID.String()); err != nil {
		t.Fatal(err)
	}
	for id, key := range map[uuid.UUID]string{leadID: "lead", workerID: "worker"} {
		if _, err := db.Exec(`INSERT INTO agents(id,agent_key,owner_id,provider,model,tenant_id) VALUES(?,?,?,'openai','test',?)`, id, key, "owner", tenantID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO agent_teams(id,name,lead_agent_id,status,settings,created_by,tenant_id) VALUES(?,?,?,'active','{"version":2}','owner',?)`, teamID, "Team", leadID, tenantID); err != nil {
		t.Fatal(err)
	}
	for id, role := range map[uuid.UUID]string{leadID: "lead", workerID: "member"} {
		if _, err := db.Exec(`INSERT INTO agent_team_members(team_id,agent_id,role,tenant_id) VALUES(?,?,?,?)`, teamID, id, role, tenantID); err != nil {
			t.Fatal(err)
		}
	}
	return NewSQLiteTeamStore(db), store.WithTenantID(context.Background(), tenantID), leadID, workerID, teamID
}

func TestSQLiteGetTeamHydratesLeadIdentityForWorkflowReplan(t *testing.T) {
	s, ctx, leadID, _, teamID := sqliteRecoveryFixture(t)

	team, err := s.GetTeam(ctx, teamID)
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if team == nil {
		t.Fatal("GetTeam returned nil")
	}
	if team.LeadAgentID != leadID {
		t.Fatalf("LeadAgentID = %s, want %s", team.LeadAgentID, leadID)
	}
	if team.LeadAgentKey != "lead" {
		t.Fatalf("LeadAgentKey = %q, want %q; workflow replan would falsely report coordinator changed", team.LeadAgentKey, "lead")
	}
}

func sqliteMakeRunningWorkflow(t *testing.T, s *SQLiteTeamStore, ctx context.Context, teamID, leadID uuid.UUID, tasks []store.TeamTaskData) *store.TeamWorkflowData {
	t.Helper()
	plan := []byte(`{"schema_version":1,"goal":"recovery","steps":[]}`)
	workflow := &store.TeamWorkflowData{
		TeamID: teamID, Status: store.TeamWorkflowStatusRunning, CanonicalPlan: plan, SchemaVersion: 1,
		PlanHash: fmt.Sprintf("%x", sha256.Sum256(plan)), CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead",
		OriginAgentID: leadID, OriginAgentKey: "lead", OriginRunID: "recovery-run",
		OriginSessionKey: "agent:lead:ws:direct:user", OriginChannel: "ws", OriginChatID: "user",
	}
	if err := s.CreateWorkflowWithTasks(ctx, workflow, tasks); err != nil {
		t.Fatalf("CreateWorkflowWithTasks: %v", err)
	}
	return workflow
}

// driveToBlocked runs the real fenced path claim→accept→block and returns the
// stale attempt (whose token no longer owns the task after the block clears it).
func driveToBlocked(t *testing.T, s *SQLiteTeamStore, ctx context.Context, workflowID, taskID, teamID uuid.UUID, reason string) store.WorkflowTaskAttempt {
	t.Helper()
	token, err := s.ClaimWorkflowTaskDispatch(ctx, taskID, teamID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("claim dispatch: %v", err)
	}
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	attempt := store.WorkflowTaskAttempt{TeamID: teamID, WorkflowID: workflowID, TaskID: taskID, DispatchToken: token, PlanRevision: task.PlanRevision, WorkflowStep: task.WorkflowStepID}
	if tr, err := s.AcceptWorkflowTaskAttempt(ctx, attempt, time.Now().Add(time.Hour)); err != nil || !tr.Applied() {
		t.Fatalf("accept attempt outcome=%v err=%v", tr.Outcome, err)
	}
	if tr, err := s.BlockWorkflowTaskAttempt(ctx, attempt, reason); err != nil || !tr.Applied() {
		t.Fatalf("block attempt outcome=%v err=%v", tr.Outcome, err)
	}
	return attempt
}

// TestSQLiteWorkflowEscalationRetryPersistsUntilResolution proves the durable
// coordinator-escalation ledger survives an enqueue/recovery-run failure: each
// ClaimTaskEscalation reschedules the next capped-backoff retry, so a dropped
// hand-off leaves the escalation armed and re-claimable rather than silently
// lost. Only a real coordinator resolution (retry/replan/cancel) clears it.
func TestSQLiteWorkflowEscalationRetryPersistsUntilResolution(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
	rootID := uuid.New()
	workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: rootID}, TeamID: teamID, Subject: "Root", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "root", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})
	driveToBlocked(t, teamStore, ctx, workflow.ID, rootID, teamID, "needs API key")

	// Blocking arms a durable escalation-pending state with a due next_at.
	blocked, err := teamStore.GetTask(ctx, rootID)
	if err != nil || blocked.EscalationStatus != store.TeamTaskEscalationPending || blocked.EscalationNextAt == nil {
		t.Fatalf("blocked escalation state=%+v err=%v", blocked, err)
	}

	crossCtx := store.WithCrossTenant(ctx)
	// The recovery ticker sweep surfaces the due escalation.
	due, err := teamStore.ListEscalationDueTasks(crossCtx, blocked.EscalationNextAt.Add(time.Second))
	if err != nil || !sqliteContainsTask(due, rootID) {
		t.Fatalf("escalation must be due for the sweep: due=%d err=%v", len(due), err)
	}

	// First claim: bump attempt, move to enqueuing, schedule the next re-claim.
	claim, err := teamStore.ClaimTaskEscalation(ctx, rootID, teamID, blocked.EscalationNextAt.Add(time.Second))
	if err != nil || !claim.Claimed || claim.Attempt != 1 {
		t.Fatalf("first escalation claim=%+v err=%v", claim, err)
	}
	afterClaim, err := teamStore.GetTask(ctx, rootID)
	if err != nil || afterClaim.EscalationStatus != store.TeamTaskEscalationEnqueuing || afterClaim.EscalationAttemptCount != 1 || afterClaim.EscalationNextAt == nil {
		t.Fatalf("after first claim=%+v err=%v", afterClaim, err)
	}

	// Simulate an enqueue / recovery-run failure: the caller does NOT resolve the
	// blocker. The durable escalation stays armed and re-appears once the newly
	// scheduled backoff elapses — the retry is not lost.
	if stillDue, err := teamStore.ListEscalationDueTasks(crossCtx, afterClaim.EscalationNextAt.Add(-time.Second)); err != nil || sqliteContainsTask(stillDue, rootID) {
		t.Fatalf("escalation must be hidden before its rescheduled next_at: due=%d err=%v", len(stillDue), err)
	}
	reDue, err := teamStore.ListEscalationDueTasks(crossCtx, afterClaim.EscalationNextAt.Add(time.Second))
	if err != nil || !sqliteContainsTask(reDue, rootID) {
		t.Fatalf("escalation must be re-claimable after its rescheduled next_at: due=%d err=%v", len(reDue), err)
	}
	claim2, err := teamStore.ClaimTaskEscalation(ctx, rootID, teamID, afterClaim.EscalationNextAt.Add(time.Second))
	if err != nil || !claim2.Claimed || claim2.Attempt != 2 {
		t.Fatalf("second escalation claim=%+v err=%v", claim2, err)
	}

	// A real coordinator resolution clears the escalation ledger for good.
	if tr, err := teamStore.RetryBlockedWorkflowTask(ctx, rootID, teamID, "use the staging key"); err != nil || !tr.Applied() {
		t.Fatalf("retry transition=%+v err=%v", tr, err)
	}
	resolved, err := teamStore.GetTask(ctx, rootID)
	if err != nil || resolved.EscalationStatus != store.TeamTaskEscalationDelivered || resolved.Status != store.TeamTaskStatusPending {
		t.Fatalf("resolved escalation state=%+v err=%v", resolved, err)
	}
	// After resolution the escalation is no longer due to any sweep.
	if gone, err := teamStore.ListEscalationDueTasks(crossCtx, time.Now().Add(time.Hour)); err != nil || sqliteContainsTask(gone, rootID) {
		t.Fatalf("resolved escalation must never be due again: due=%d err=%v", len(gone), err)
	}
}

func sqliteContainsTask(tasks []store.TeamTaskData, id uuid.UUID) bool {
	for _, task := range tasks {
		if task.ID == id {
			return true
		}
	}
	return false
}

// TestSQLiteWorkflowPersistsClassificationAuditLink is the SQLite twin: it
// proves the classification audit ID set on a workflow is written by the INSERT
// and read back by the SELECT (the last hop gate → run ctx → builder → DB). A
// prior INSERT omission left this column NULL despite the builder stamping it.
func TestSQLiteWorkflowPersistsClassificationAuditLink(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)

	audit := &store.TeamWorkClassificationAudit{Ingress: store.TeamWorkIngressWS, AgentID: &leadID}
	if err := teamStore.RecordTeamWorkClassificationAudit(ctx, audit); err != nil {
		t.Fatalf("record audit: %v", err)
	}
	if audit.ID == uuid.Nil {
		t.Fatal("audit ID must be populated")
	}

	rootID := uuid.New()
	plan := []byte(`{"schema_version":1,"goal":"audited","steps":[]}`)
	workflow := &store.TeamWorkflowData{
		TeamID: teamID, Status: store.TeamWorkflowStatusRunning, CanonicalPlan: plan, SchemaVersion: 1,
		PlanHash: fmt.Sprintf("%x", sha256.Sum256(plan)), CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead",
		OriginAgentID: leadID, OriginAgentKey: "lead", OriginRunID: "audited-run",
		OriginSessionKey: "agent:lead:ws:direct:user", OriginChannel: "ws", OriginChatID: "user",
		ClassificationAuditID: &audit.ID,
	}
	if err := teamStore.CreateWorkflowWithTasks(ctx, workflow, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: rootID}, TeamID: teamID, Subject: "Root", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "root", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	}); err != nil {
		t.Fatalf("CreateWorkflowWithTasks: %v", err)
	}

	got, err := teamStore.GetWorkflow(ctx, workflow.ID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if got.ClassificationAuditID == nil {
		t.Fatal("classification_audit_id must survive the INSERT and be read back (was NULL)")
	}
	if *got.ClassificationAuditID != audit.ID {
		t.Fatalf("audit link = %s, want %s", *got.ClassificationAuditID, audit.ID)
	}
}

func TestSQLiteRetryBlockedWorkflowTaskResetsOnlyAffectedTask(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
	rootID, terminalID := uuid.New(), uuid.New()
	workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: rootID}, TeamID: teamID, Subject: "Root", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "root", WorkflowKind: store.TeamWorkflowTaskKindWork},
		{BaseModel: store.BaseModel{ID: terminalID}, TeamID: teamID, Subject: "Terminal", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "terminal", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})

	staleAttempt := driveToBlocked(t, teamStore, ctx, workflow.ID, rootID, teamID, "needs API key")

	// Blocking arms a durable escalation-pending state and does NOT fail the
	// workflow or touch the independent terminal task.
	blocked, err := teamStore.GetTask(ctx, rootID)
	if err != nil || blocked.Status != store.TeamTaskStatusBlocked || blocked.EscalationStatus != store.TeamTaskEscalationPending || blocked.BlockerReason != "needs API key" {
		t.Fatalf("blocked task=%+v err=%v", blocked, err)
	}
	if wf, err := teamStore.GetWorkflow(ctx, workflow.ID); err != nil || wf.Status != store.TeamWorkflowStatusRunning {
		t.Fatalf("workflow status=%v err=%v, want running", wf.Status, err)
	}
	if other, err := teamStore.GetTask(ctx, terminalID); err != nil || other.Status != store.TeamTaskStatusPending {
		t.Fatalf("independent terminal task=%v err=%v, want untouched pending", other.Status, err)
	}

	// Coordinator retries the blocked task with a revised instruction.
	tr, err := teamStore.RetryBlockedWorkflowTask(ctx, rootID, teamID, "use the staging key")
	if err != nil || !tr.Applied() || tr.TaskStatus != store.TeamTaskStatusPending || tr.WorkflowID != workflow.ID {
		t.Fatalf("retry transition=%+v err=%v", tr, err)
	}
	retried, err := teamStore.GetTask(ctx, rootID)
	if err != nil || retried.Status != store.TeamTaskStatusPending || retried.RecoveryCount != 1 || retried.BlockerReason != "" || retried.EscalationStatus != store.TeamTaskEscalationDelivered {
		t.Fatalf("retried task=%+v err=%v", retried, err)
	}
	if retried.OwnerAgentID == nil || *retried.OwnerAgentID != workerID {
		t.Fatalf("retry must preserve owner, got %v", retried.OwnerAgentID)
	}

	// The superseded attempt cannot revive the task: heartbeat/complete are stale.
	if hb, err := teamStore.HeartbeatWorkflowTaskAttempt(ctx, staleAttempt, time.Now().Add(time.Hour)); err != nil || !hb.Stale() {
		t.Fatalf("stale heartbeat outcome=%v err=%v", hb.Outcome, err)
	}
	if cp, err := teamStore.CompleteWorkflowTaskAttempt(ctx, staleAttempt, "late result"); err != nil || !cp.Stale() {
		t.Fatalf("stale complete outcome=%v err=%v", cp.Outcome, err)
	}
	if still, err := teamStore.GetTask(ctx, rootID); err != nil || still.Status != store.TeamTaskStatusPending {
		t.Fatalf("task must stay pending after stale attempts, got %v err=%v", still.Status, err)
	}

	// Retrying a non-blocked task is a no-op stale transition.
	if tr, err := teamStore.RetryBlockedWorkflowTask(ctx, rootID, teamID, ""); err != nil || tr.Outcome != store.WorkflowMutationStale {
		t.Fatalf("retry of non-blocked task outcome=%v err=%v", tr.Outcome, err)
	}
}

func TestSQLiteCommitWorkflowReplanSupersedesRevision(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
	db := teamStore.db
	doneID, blockedID, inReviewID := uuid.New(), uuid.New(), uuid.New()
	workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: doneID}, TeamID: teamID, Subject: "Completed evidence", Status: store.TeamTaskStatusCompleted, Result: sqliteRecoveryStrPtr("gathered"), OwnerAgentID: &workerID, WorkflowStepID: "gather", WorkflowKind: store.TeamWorkflowTaskKindWork},
		{BaseModel: store.BaseModel{ID: blockedID}, TeamID: teamID, Subject: "Blocked terminal", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "deliver", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
		{BaseModel: store.BaseModel{ID: inReviewID}, TeamID: teamID, Subject: "In Review task", Status: store.TeamTaskStatusInReview, OwnerAgentID: &workerID, WorkflowStepID: "review", WorkflowKind: store.TeamWorkflowTaskKindWork},
	})

	if _, err := db.Exec(`UPDATE team_workflows SET status = ? WHERE id = ?`, store.TeamWorkflowStatusNeedsRevision, workflow.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE team_tasks SET status = ?, escalation_status = 'pending', escalation_attempt_count = 3, blocker_reason = 'waiting' WHERE id = ?`,
		store.TeamTaskStatusBlocked, blockedID); err != nil {
		t.Fatal(err)
	}
	tok := uuid.New()
	future := time.Now().Add(time.Hour)
	if _, err := db.Exec(`UPDATE team_tasks SET dispatch_token = ?, dispatch_lease_until = ?, locked_at = ?, lock_expires_at = ? WHERE id = ?`,
		tok, future, future, future, inReviewID); err != nil {
		t.Fatal(err)
	}

	newTaskID := uuid.New()
	newPlan := []byte(`{"schema_version":1,"goal":"recovery","steps":["revised"]}`)
	replan := store.WorkflowReplan{
		Guard: store.WorkflowActionGuard{
			Action:               store.WorkflowActionApplyReplan,
			TeamID:               teamID,
			WorkflowID:           workflow.ID,
			ExpectedPlanRevision: 1,
			ExpectedStatus:       store.TeamWorkflowStatusNeedsRevision,
			TaskID:               &blockedID,
			ExpectedTaskStatus:   store.TeamTaskStatusBlocked,
			Reason:               "replan justification",
			Actor: store.WorkflowActionActor{
				Kind:    store.WorkflowActorCoordinator,
				AgentID: &leadID,
			},
		},
		CoordinatorID: leadID,
		CanonicalPlan: newPlan,
		PlanHash:      fmt.Sprintf("%x", sha256.Sum256(newPlan)),
		Tasks: []store.TeamTaskData{
			{BaseModel: store.BaseModel{ID: newTaskID}, TeamID: teamID, Subject: "Revised terminal", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "deliver", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
		},
	}

	badCoord := replan
	badCoord.CoordinatorID = workerID
	resCoord, err := teamStore.CommitWorkflowReplan(ctx, badCoord)
	if err != nil {
		t.Fatalf("expected no error on coordinator mismatch Conflict, got %v", err)
	}
	if resCoord.Outcome != store.WorkflowActionConflict {
		t.Fatalf("expected Conflict on coordinator mismatch, got %v", resCoord.Outcome)
	}

	res, err := teamStore.CommitWorkflowReplan(ctx, replan)
	if err != nil {
		t.Fatalf("replan failed: %v", err)
	}
	if res.Outcome != store.WorkflowActionApplied {
		t.Fatalf("expected Outcome Applied, got %v", res.Outcome)
	}
	if res.Workflow.Status != store.TeamWorkflowStatusRunning || res.Workflow.PlanRevision != 2 {
		t.Fatalf("workflow status/revision mismatch: %+v", res.Workflow)
	}

	if done, err := teamStore.GetTask(ctx, doneID); err != nil || done.Status != store.TeamTaskStatusCompleted || done.Result == nil || *done.Result != "gathered" {
		t.Fatalf("completed evidence changed: %+v err=%v", done, err)
	}

	if old, err := teamStore.GetTask(ctx, blockedID); err != nil || old.Status != store.TeamTaskStatusStale {
		t.Fatalf("old non-terminal task=%v err=%v, want stale", old.Status, err)
	} else {
		if old.DispatchToken != nil || old.LockedAt != nil || old.BlockerReason != "" || old.EscalationStatus != store.TeamTaskEscalationDelivered || old.EscalationAttemptCount != 0 {
			t.Fatalf("old task states not cleared: %+v", old)
		}
	}

	if inRev, err := teamStore.GetTask(ctx, inReviewID); err != nil || inRev.Status != store.TeamTaskStatusStale {
		t.Fatalf("old in_review task status = %v err = %v, want stale", inRev.Status, err)
	} else {
		if inRev.DispatchToken != nil || inRev.LockedAt != nil {
			t.Fatalf("old in_review task states not cleared: %+v", inRev)
		}
	}

	if fresh, err := teamStore.GetTask(ctx, newTaskID); err != nil || fresh.PlanRevision != 2 || fresh.Status != store.TeamTaskStatusPending {
		t.Fatalf("new-revision task=%+v err=%v", fresh, err)
	}

	comments, err := teamStore.ListTaskComments(ctx, blockedID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 || comments[0].Content != "replan justification" || comments[0].CommentType != "recovery" {
		t.Fatalf("expected 1 recovery comment, got: %+v", comments)
	}

	resStale, err := teamStore.CommitWorkflowReplan(ctx, replan)
	if err != nil {
		t.Fatalf("expected no error on stale revision Conflict, got %v", err)
	}
	if resStale.Outcome != store.WorkflowActionConflict {
		t.Fatalf("expected Conflict on stale revision, got %v", resStale.Outcome)
	}

	commentsAfter, err := teamStore.ListTaskComments(ctx, blockedID)
	if err != nil {
		t.Fatal(err)
	}
	if len(commentsAfter) != 1 {
		t.Fatalf("expected no extra comments on Conflict, got %d", len(commentsAfter))
	}
}

func TestSQLiteCommitWorkflowReplanRollsBackAfterReservationError(t *testing.T) {
	teamStore, ctx, leadID, _, teamID := sqliteRecoveryFixture(t)
	blockedID := uuid.New()
	workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: blockedID}, TeamID: teamID, Subject: "Blocked terminal", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "deliver", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})
	if _, err := teamStore.db.Exec(`UPDATE team_workflows SET status = ? WHERE id = ?`, store.TeamWorkflowStatusNeedsRevision, workflow.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := teamStore.db.Exec(`UPDATE team_tasks SET status = ?, blocker_reason = 'waiting', escalation_status = 'pending' WHERE id = ?`, store.TeamTaskStatusBlocked, blockedID); err != nil {
		t.Fatal(err)
	}
	if _, err := teamStore.db.Exec(`CREATE TRIGGER reject_replan_task BEFORE INSERT ON team_tasks
		WHEN NEW.workflow_id = '` + workflow.ID.String() + `' AND NEW.plan_revision = 2
		BEGIN SELECT RAISE(ABORT, 'forced replacement insert failure'); END`); err != nil {
		t.Fatal(err)
	}

	newTaskID := uuid.New()
	newPlan := []byte(`{"schema_version":1,"goal":"recovery","steps":["revised"]}`)
	replan := store.WorkflowReplan{
		Guard: store.WorkflowActionGuard{
			Action:               store.WorkflowActionApplyReplan,
			TeamID:               teamID,
			WorkflowID:           workflow.ID,
			ExpectedPlanRevision: 1,
			ExpectedStatus:       store.TeamWorkflowStatusNeedsRevision,
			TaskID:               &blockedID,
			ExpectedTaskStatus:   store.TeamTaskStatusBlocked,
			Reason:               "replan justification",
			Actor:                store.WorkflowActionActor{Kind: store.WorkflowActorCoordinator, AgentID: &leadID},
		},
		CoordinatorID: leadID,
		CanonicalPlan: newPlan,
		PlanHash:      fmt.Sprintf("%x", sha256.Sum256(newPlan)),
		Tasks: []store.TeamTaskData{
			{BaseModel: store.BaseModel{ID: newTaskID}, TeamID: teamID, Subject: "Revised terminal", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "deliver", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
		},
	}
	if _, err := teamStore.CommitWorkflowReplan(ctx, replan); err == nil || !strings.Contains(err.Error(), "forced replacement insert failure") {
		t.Fatalf("CommitWorkflowReplan() error = %v, want forced insert failure", err)
	}

	persisted, err := teamStore.GetWorkflow(ctx, workflow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != store.TeamWorkflowStatusNeedsRevision || persisted.PlanRevision != 1 {
		t.Fatalf("workflow changed after rollback: status=%q revision=%d", persisted.Status, persisted.PlanRevision)
	}
	oldTask, err := teamStore.GetTask(ctx, blockedID)
	if err != nil {
		t.Fatal(err)
	}
	if oldTask.Status != store.TeamTaskStatusBlocked || oldTask.BlockerReason != "waiting" || oldTask.EscalationStatus != store.TeamTaskEscalationPending {
		t.Fatalf("blocked task changed after rollback: %+v", oldTask)
	}
	if _, err := teamStore.GetTask(ctx, newTaskID); err == nil {
		t.Fatal("replacement task persisted despite rollback")
	}
	comments, err := teamStore.ListTaskComments(ctx, blockedID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 0 {
		t.Fatalf("rollback persisted %d comments", len(comments))
	}
}

func TestSQLiteCommitWorkflowReplanConcurrency(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
	db := teamStore.db
	doneID, blockedID := uuid.New(), uuid.New()
	workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: doneID}, TeamID: teamID, Subject: "Completed evidence", Status: store.TeamTaskStatusCompleted, Result: sqliteRecoveryStrPtr("gathered"), OwnerAgentID: &workerID, WorkflowStepID: "gather", WorkflowKind: store.TeamWorkflowTaskKindWork},
		{BaseModel: store.BaseModel{ID: blockedID}, TeamID: teamID, Subject: "Blocked terminal", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "deliver", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})

	if _, err := db.Exec(`UPDATE team_workflows SET status = ? WHERE id = ?`, store.TeamWorkflowStatusNeedsRevision, workflow.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE team_tasks SET status = ? WHERE id = ?`, store.TeamTaskStatusBlocked, blockedID); err != nil {
		t.Fatal(err)
	}

	newPlan1 := []byte(`{"schema_version":1,"goal":"recovery one","steps":["revised"]}`)
	newPlan2 := []byte(`{"schema_version":1,"goal":"recovery two","steps":["revised"]}`)
	replan1 := store.WorkflowReplan{
		Guard: store.WorkflowActionGuard{
			Action:               store.WorkflowActionApplyReplan,
			TeamID:               teamID,
			WorkflowID:           workflow.ID,
			ExpectedPlanRevision: 1,
			ExpectedStatus:       store.TeamWorkflowStatusNeedsRevision,
			TaskID:               &blockedID,
			ExpectedTaskStatus:   store.TeamTaskStatusBlocked,
			Reason:               "winner reason",
			Actor:                store.WorkflowActionActor{Kind: store.WorkflowActorCoordinator, AgentID: &leadID},
		},
		CoordinatorID: leadID,
		CanonicalPlan: newPlan1,
		PlanHash:      fmt.Sprintf("%x", sha256.Sum256(newPlan1)),
		Tasks: []store.TeamTaskData{
			{BaseModel: store.BaseModel{ID: uuid.New()}, TeamID: teamID, Subject: "Revised terminal 1", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "deliver", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
		},
	}

	replan2 := store.WorkflowReplan{
		Guard: store.WorkflowActionGuard{
			Action:               store.WorkflowActionApplyReplan,
			TeamID:               teamID,
			WorkflowID:           workflow.ID,
			ExpectedPlanRevision: 1,
			ExpectedStatus:       store.TeamWorkflowStatusNeedsRevision,
			TaskID:               &blockedID,
			ExpectedTaskStatus:   store.TeamTaskStatusBlocked,
			Reason:               "loser reason",
			Actor:                store.WorkflowActionActor{Kind: store.WorkflowActorCoordinator, AgentID: &leadID},
		},
		CoordinatorID: leadID,
		CanonicalPlan: newPlan2,
		PlanHash:      fmt.Sprintf("%x", sha256.Sum256(newPlan2)),
		Tasks: []store.TeamTaskData{
			{BaseModel: store.BaseModel{ID: uuid.New()}, TeamID: teamID, Subject: "Revised terminal 2", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "deliver", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
		},
	}

	const concurrency = 10
	results := make(chan store.WorkflowActionResult, concurrency)
	errorsChan := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			var r store.WorkflowReplan
			if idx%2 == 0 {
				r = replan1
			} else {
				r = replan2
			}
			res, err := teamStore.CommitWorkflowReplan(ctx, r)
			if err != nil {
				errorsChan <- err
			} else {
				results <- res
			}
		}(i)
	}

	appliedCount := 0
	conflictCount := 0

	for i := 0; i < concurrency; i++ {
		select {
		case err := <-errorsChan:
			t.Fatalf("concurrent replan failed with error: %v", err)
		case res := <-results:
			if res.Outcome == store.WorkflowActionApplied {
				appliedCount++
			} else if res.Outcome == store.WorkflowActionConflict {
				conflictCount++
			}
		}
	}

	if appliedCount != 1 {
		t.Fatalf("expected exactly 1 applied replan, got %d", appliedCount)
	}
	if conflictCount != concurrency-1 {
		t.Fatalf("expected %d conflicts, got %d", concurrency-1, conflictCount)
	}

	persisted, err := teamStore.GetWorkflow(ctx, workflow.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.PlanRevision != 2 || persisted.Status != store.TeamWorkflowStatusRunning {
		t.Fatalf("persisted workflow=%+v, want running revision 2", persisted)
	}
	winnerTaskID, loserTaskID := replan1.Tasks[0].ID, replan2.Tasks[0].ID
	switch persisted.PlanHash {
	case replan1.PlanHash:
		if string(persisted.CanonicalPlan) != string(replan1.CanonicalPlan) {
			t.Fatal("winning replan hash does not match persisted canonical bytes")
		}
	case replan2.PlanHash:
		winnerTaskID, loserTaskID = loserTaskID, winnerTaskID
		if string(persisted.CanonicalPlan) != string(replan2.CanonicalPlan) {
			t.Fatal("winning replan hash does not match persisted canonical bytes")
		}
	default:
		t.Fatalf("persisted unknown plan hash %q", persisted.PlanHash)
	}
	tasks, err := teamStore.ListWorkflowTasks(ctx, workflow.ID)
	if err != nil {
		t.Fatal(err)
	}
	revisionTwo := make(map[uuid.UUID]store.TeamTaskData)
	for _, task := range tasks {
		if task.PlanRevision == 2 {
			revisionTwo[task.ID] = task
		}
	}
	if len(revisionTwo) != 1 {
		t.Fatalf("persisted revision-2 graphs contain %d tasks, want exactly 1", len(revisionTwo))
	}
	if _, ok := revisionTwo[winnerTaskID]; !ok {
		t.Fatalf("winning replacement task %s was not persisted", winnerTaskID)
	}
	if _, ok := revisionTwo[loserTaskID]; ok {
		t.Fatalf("losing replacement task %s was persisted", loserTaskID)
	}
	if old, err := teamStore.GetTask(ctx, blockedID); err != nil || old.Status != store.TeamTaskStatusStale {
		t.Fatalf("old mutable task=%+v err=%v, want stale", old, err)
	}
	comments, err := teamStore.ListTaskComments(ctx, blockedID)
	if err != nil {
		t.Fatal(err)
	}
	if len(comments) != 1 {
		t.Fatalf("replan race persisted %d recovery comments, want 1", len(comments))
	}
}

func TestSQLiteCancelWorkflowCancelsNonTerminalTasks(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
	doneID, activeID := uuid.New(), uuid.New()
	workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: doneID}, TeamID: teamID, Subject: "Completed", Status: store.TeamTaskStatusCompleted, Result: sqliteRecoveryStrPtr("kept"), OwnerAgentID: &workerID, WorkflowStepID: "one", WorkflowKind: store.TeamWorkflowTaskKindWork},
		{BaseModel: store.BaseModel{ID: activeID}, TeamID: teamID, Subject: "Active", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "two", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})

	cancelled, err := teamStore.CancelWorkflow(ctx, workflow.ID, teamID, "user aborted")
	if err != nil || cancelled.Status != store.TeamWorkflowStatusCancelling || cancelled.CancelReason != "user aborted" {
		t.Fatalf("cancel workflow=%+v err=%v", cancelled, err)
	}
	if done, err := teamStore.GetTask(ctx, doneID); err != nil || done.Status != store.TeamTaskStatusCompleted || (done.Result == nil || *done.Result != "kept") {
		t.Fatalf("completed result must survive cancel: %+v err=%v", done, err)
	}
	if active, err := teamStore.GetTask(ctx, activeID); err != nil || active.Status != store.TeamTaskStatusCancelled {
		t.Fatalf("active task=%v err=%v, want cancelled", active.Status, err)
	}
	// A terminal workflow is no longer cancellable.
	if _, err := teamStore.CancelWorkflow(ctx, workflow.ID, teamID, "again"); err == nil {
		t.Fatal("cancelling a non-cancellable workflow must fail")
	}
}

// TestSQLiteCancelWorkflowFinalizesToCancelled proves the cancelling→cancelled
// transition is not a dead-end: after CancelWorkflow moves the workflow to
// cancelling (and cancels its non-terminal work tasks in the same transaction),
// the finalizer can discover it via ListWorkflowsReadyToFinalize, claim it, and
// commit the terminal cancelled status with a durable summary.
func TestSQLiteCancelWorkflowFinalizesToCancelled(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
	doneID, activeID := uuid.New(), uuid.New()
	workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: doneID}, TeamID: teamID, Subject: "Completed", Status: store.TeamTaskStatusCompleted, Result: sqliteRecoveryStrPtr("kept"), OwnerAgentID: &workerID, WorkflowStepID: "one", WorkflowKind: store.TeamWorkflowTaskKindWork},
		{BaseModel: store.BaseModel{ID: activeID}, TeamID: teamID, Subject: "Active", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "two", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})

	if _, err := teamStore.CancelWorkflow(ctx, workflow.ID, teamID, "user aborted"); err != nil {
		t.Fatalf("cancel workflow: %v", err)
	}

	// The finalizer ticker scans cross-tenant. A cancelling workflow with no active
	// dispatching/in_progress work tasks is ready to finalize (CancelWorkflow already
	// cancelled the only non-terminal task in-transaction).
	crossCtx := store.WithCrossTenant(ctx)
	ready, err := teamStore.ListWorkflowsReadyToFinalize(crossCtx, time.Now())
	if err != nil {
		t.Fatalf("list ready: %v", err)
	}
	found := false
	for _, scope := range ready {
		if scope.WorkflowID == workflow.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("cancelling workflow must be ready to finalize")
	}

	fin, token, err := teamStore.ClaimWorkflowFinalization(ctx, workflow.ID, time.Now().Add(time.Minute))
	if err != nil || fin == nil || fin.Status != store.TeamWorkflowStatusCancelling {
		t.Fatalf("claim finalization=%+v err=%v", fin, err)
	}
	if err := teamStore.CompleteWorkflowFinalization(ctx, workflow.ID, token, store.TeamWorkflowStatusCancelled, "Cancelled: user aborted"); err != nil {
		t.Fatalf("complete finalization: %v", err)
	}
	final, err := teamStore.GetWorkflow(ctx, workflow.ID)
	if err != nil || final.Status != store.TeamWorkflowStatusCancelled || final.ResultSummary != "Cancelled: user aborted" || final.FinalizedAt == nil {
		t.Fatalf("finalized workflow=%+v err=%v", final, err)
	}
	// Completed evidence stays immutable through the whole cancel→finalize path.
	if done, err := teamStore.GetTask(ctx, doneID); err != nil || done.Status != store.TeamTaskStatusCompleted || done.Result == nil || *done.Result != "kept" {
		t.Fatalf("completed result must survive cancel/finalize: %+v err=%v", done, err)
	}
}

func TestSQLiteFailWorkflowExpansionBoundedBackoffThenFailing(t *testing.T) {
	teamStore, ctx, leadID, _, teamID := sqliteRecoveryFixture(t)
	plan := []byte(`{"schema_version":1,"goal":"expand","steps":[]}`)
	pending := &store.TeamWorkflowData{
		TeamID: teamID, Status: store.TeamWorkflowStatusPendingExpansion, CanonicalPlan: plan, SchemaVersion: 1,
		PlanHash: fmt.Sprintf("%x", sha256.Sum256(plan)), CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead",
		OriginAgentID: leadID, OriginAgentKey: "lead", OriginRunID: "expand-run",
		OriginSessionKey: "agent:lead:ws:direct:user", OriginChannel: "ws", OriginChatID: "user",
		AutoExpand: true, // the ticker only auto-expands workflows flagged for it
	}
	audit := &store.TeamTaskData{TeamID: teamID, Subject: "Request", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, TaskType: "request", CreatedByAgentID: &leadID}
	if err := teamStore.CreatePendingWorkflowRequest(ctx, pending, audit); err != nil {
		t.Fatalf("CreatePendingWorkflowRequest: %v", err)
	}

	// A transient failure schedules a capped-backoff retry: still pending_expansion,
	// attempt counted, token cleared and next_expansion_at armed.
	token, err := teamStore.ClaimPendingWorkflowExpansion(ctx, pending.ID, leadID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("claim expansion: %v", err)
	}
	w, err := teamStore.FailWorkflowExpansion(ctx, pending.ID, leadID, token, "provider timeout", true)
	if err != nil || w.Status != store.TeamWorkflowStatusPendingExpansion || w.ExpansionAttemptCount != 1 || w.NextExpansionAt == nil || w.LastExpansionError != "provider timeout" {
		t.Fatalf("transient expansion fail=%+v err=%v", w, err)
	}
	// A stale token cannot consume an attempt.
	if _, err := teamStore.FailWorkflowExpansion(ctx, pending.ID, leadID, token, "stale", true); err == nil {
		t.Fatal("stale expansion token must be rejected")
	}
	// The ticker query honors the backoff: the workflow is hidden until
	// next_expansion_at, then reappears. This is what stops the pre-fix tight retry.
	crossCtx := store.WithCrossTenant(ctx)
	if due, err := teamStore.ListPendingAutoExpandWorkflows(crossCtx, w.NextExpansionAt.Add(-time.Second)); err != nil || sqliteContainsWorkflow(due, pending.ID) {
		t.Fatalf("expansion must be hidden before next_expansion_at: due=%d err=%v", len(due), err)
	}
	if due, err := teamStore.ListPendingAutoExpandWorkflows(crossCtx, w.NextExpansionAt.Add(time.Second)); err != nil || !sqliteContainsWorkflow(due, pending.ID) {
		t.Fatalf("expansion must be due after next_expansion_at: due=%d err=%v", len(due), err)
	}

	// A deterministic (non-transient) failure exhausts immediately → failing with a
	// user-visible failure summary.
	token, err = teamStore.ClaimPendingWorkflowExpansion(ctx, pending.ID, leadID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("re-claim expansion: %v", err)
	}
	w, err = teamStore.FailWorkflowExpansion(ctx, pending.ID, leadID, token, "roster invalidated", false)
	if err != nil || w.Status != store.TeamWorkflowStatusFailing || w.FailureSummary == "" {
		t.Fatalf("non-transient expansion fail=%+v err=%v", w, err)
	}
}

func TestSQLiteFailWorkflowDeliveryAttemptBoundedThenDead(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
	workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: uuid.New()}, TeamID: teamID, Subject: "Only", Status: store.TeamTaskStatusCompleted, Result: sqliteRecoveryStrPtr("done"), OwnerAgentID: &workerID, WorkflowStepID: "only", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})
	// Mark the workflow finalized so delivery can be claimed (the finalizer path is
	// exercised by TestSQLiteWorkflowDurabilityAndDispatchCAS; here we isolate the
	// bounded retry ledger).
	if err := teamStore.CompleteWorkflowFinalization(ctx, workflow.ID, uuid.Nil, store.TeamWorkflowStatusCompleted, "final result"); err == nil {
		t.Fatal("finalization with a nil token must fail")
	}
	fin, finToken, err := teamStore.ClaimWorkflowFinalization(ctx, workflow.ID, time.Now().Add(time.Minute))
	if err != nil || fin == nil {
		t.Fatalf("claim finalization: %v", err)
	}
	if err := teamStore.CompleteWorkflowFinalization(ctx, workflow.ID, finToken, store.TeamWorkflowStatusCompleted, "final result"); err != nil {
		t.Fatalf("complete finalization: %v", err)
	}

	var lastStatus string
	for i := 1; i <= store.MaxWorkflowDeliveryAttempts; i++ {
		_, token, err := teamStore.ClaimWorkflowDelivery(ctx, workflow.ID, time.Now().Add(time.Minute))
		if err != nil {
			t.Fatalf("claim delivery attempt %d: %v", i, err)
		}
		w, err := teamStore.FailWorkflowDeliveryAttempt(ctx, workflow.ID, token, fmt.Sprintf("channel down %d", i))
		if err != nil {
			t.Fatalf("fail delivery attempt %d: %v", i, err)
		}
		if w.DeliveryAttemptCount != i {
			t.Fatalf("attempt %d: delivery_attempt_count=%d", i, w.DeliveryAttemptCount)
		}
		lastStatus = w.DeliveryStatus
		if i < store.MaxWorkflowDeliveryAttempts {
			if w.DeliveryStatus != store.TeamWorkflowDeliveryPending || w.NextDeliveryAt == nil {
				t.Fatalf("attempt %d must schedule retry: status=%q next=%v", i, w.DeliveryStatus, w.NextDeliveryAt)
			}
			// The finalize re-drive query honors the delivery backoff: hidden before
			// next_delivery_at, due after. This is what stops the pre-fix tight re-claim.
			crossCtx := store.WithCrossTenant(ctx)
			if due, err := teamStore.ListWorkflowsReadyToFinalize(crossCtx, w.NextDeliveryAt.Add(-time.Second)); err != nil || sqliteContainsScope(due, workflow.ID) {
				t.Fatalf("attempt %d: delivery must be hidden before next_delivery_at: ready=%d err=%v", i, len(due), err)
			}
			if due, err := teamStore.ListWorkflowsReadyToFinalize(crossCtx, w.NextDeliveryAt.Add(time.Second)); err != nil || !sqliteContainsScope(due, workflow.ID) {
				t.Fatalf("attempt %d: delivery must be due after next_delivery_at: ready=%d err=%v", i, len(due), err)
			}
			if _, err := teamStore.db.Exec(`UPDATE team_workflows SET next_delivery_at=? WHERE id=?`, time.Now().Add(-time.Second), workflow.ID); err != nil {
				t.Fatalf("make delivery attempt %d due: %v", i+1, err)
			}
		}
	}
	if lastStatus != store.TeamWorkflowDeliveryDead {
		t.Fatalf("delivery must be dead after %d attempts, got %q", store.MaxWorkflowDeliveryAttempts, lastStatus)
	}
	// The result summary stays readable via the API/UI even when delivery is dead.
	if w, err := teamStore.GetWorkflow(ctx, workflow.ID); err != nil || w.ResultSummary != "final result" || w.LastDeliveryError == "" {
		t.Fatalf("dead-delivery workflow=%+v err=%v", w, err)
	}
	// A dead delivery is permanently excluded from the finalize re-drive so the
	// operator-visible dead state is terminal, not re-claimed forever.
	crossCtx := store.WithCrossTenant(ctx)
	if due, err := teamStore.ListWorkflowsReadyToFinalize(crossCtx, time.Now().Add(time.Hour)); err != nil || sqliteContainsScope(due, workflow.ID) {
		t.Fatalf("dead delivery must never be re-claimed: ready=%d err=%v", len(due), err)
	}
}

func sqliteContainsWorkflow(workflows []store.TeamWorkflowData, id uuid.UUID) bool {
	for _, w := range workflows {
		if w.ID == id {
			return true
		}
	}
	return false
}

func sqliteContainsScope(scopes []store.TeamWorkflowDispatchScope, id uuid.UUID) bool {
	for _, s := range scopes {
		if s.WorkflowID == id {
			return true
		}
	}
	return false
}

// TestSQLiteTaskReadPopulatesEventBuilderFields proves a SQLite task read
// carries every field BuildTeamTaskEventPayload derives from the committed row:
// the owner agent_key (resolved via the agents JOIN, never a UUID), workflow ID,
// workflow step ID, and plan revision. This is the store half of Item 1 case (7)
// — the authoritative event payload is only correct if the read populates these.
func TestSQLiteTaskReadPopulatesEventBuilderFields(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
	rootID := uuid.New()
	workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: rootID}, TeamID: teamID, Subject: "Root", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "root", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})

	task, err := teamStore.GetTask(ctx, rootID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	// The JOIN must resolve the owner agent_key (the worker fixture uses "worker").
	if task.OwnerAgentKey != "worker" {
		t.Fatalf("owner agent_key = %q, want %q (agents JOIN)", task.OwnerAgentKey, "worker")
	}
	if task.WorkflowID == nil || *task.WorkflowID != workflow.ID {
		t.Fatalf("workflow ID = %v, want %s", task.WorkflowID, workflow.ID)
	}
	if task.WorkflowStepID != "root" || task.PlanRevision != 1 {
		t.Fatalf("workflow step/revision = %q/%d, want root/1", task.WorkflowStepID, task.PlanRevision)
	}

	// The builder consumes exactly these fields; verify they flow through.
	payload := tools.BuildTeamTaskEventPayload(task, "Worker Display", tools.TeamTaskEventOptions{ActorType: "system"})
	if payload.OwnerAgentKey != "worker" || payload.OwnerDisplayName != "Worker Display" {
		t.Fatalf("payload owner = %q/%q, want worker/Worker Display", payload.OwnerAgentKey, payload.OwnerDisplayName)
	}
	if payload.WorkflowID != workflow.ID.String() || payload.WorkflowStepID != "root" || payload.PlanRevision != 1 {
		t.Fatalf("payload workflow fields = %q/%q/%d", payload.WorkflowID, payload.WorkflowStepID, payload.PlanRevision)
	}
	if payload.TeamID != teamID.String() || payload.TaskID != rootID.String() || payload.Status != store.TeamTaskStatusPending {
		t.Fatalf("payload identity = %+v", payload)
	}
}

func TestSQLiteWorkflowTaskConsistencyConstraint(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	// A workflow work task without a step ID must be rejected by the DB guard.
	_, err := db.Exec(`INSERT INTO team_tasks(id,team_id,tenant_id,subject,status,blocked_by,metadata,task_type,workflow_id,workflow_kind) VALUES(?,?,?,?,?,'[]','{}','general',?,'work')`, uuid.New(), uuid.New(), uuid.New(), "invalid", "pending", uuid.New())
	if err == nil || (!strings.Contains(err.Error(), "invalid workflow task fields") && !strings.Contains(err.Error(), "CHECK constraint failed")) {
		t.Fatalf("invalid workflow task fields error=%v", err)
	}
}

// A TRANSIENT provider failure on a running step must NOT be terminal. Requeueing
// returns the step to pending, keeps the workflow running, and preserves every
// already-completed step — the whole point: one router timeout on the last step
// used to discard all the work before it. dispatch_count is preserved so
// maxTaskDispatches still bounds retries.
func TestSQLiteRequeueWorkflowTaskAttemptKeepsWorkflowRunning(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
	doneID, lastID := uuid.New(), uuid.New()
	workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
		{BaseModel: store.BaseModel{ID: doneID}, TeamID: teamID, Subject: "Done", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "done", WorkflowKind: store.TeamWorkflowTaskKindWork},
		{BaseModel: store.BaseModel{ID: lastID}, TeamID: teamID, Subject: "Last", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "last", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
	})

	// First step completes for real.
	firstToken, err := teamStore.ClaimWorkflowTaskDispatch(ctx, doneID, teamID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("claim first: %v", err)
	}
	firstTask, err := teamStore.GetTask(ctx, doneID)
	if err != nil {
		t.Fatal(err)
	}
	firstAttempt := store.WorkflowTaskAttempt{TeamID: teamID, WorkflowID: workflow.ID, TaskID: doneID, DispatchToken: firstToken, PlanRevision: firstTask.PlanRevision, WorkflowStep: "done"}
	if tr, err := teamStore.AcceptWorkflowTaskAttempt(ctx, firstAttempt, time.Now().Add(time.Hour)); err != nil || !tr.Applied() {
		t.Fatalf("accept first outcome=%v err=%v", tr.Outcome, err)
	}
	if tr, err := teamStore.CompleteWorkflowTaskAttempt(ctx, firstAttempt, "real work output"); err != nil || !tr.Applied() {
		t.Fatalf("complete first outcome=%v err=%v", tr.Outcome, err)
	}

	// Terminal step starts, then its provider call dies transiently.
	lastToken, err := teamStore.ClaimWorkflowTaskDispatch(ctx, lastID, teamID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("claim last: %v", err)
	}
	lastTask, err := teamStore.GetTask(ctx, lastID)
	if err != nil {
		t.Fatal(err)
	}
	dispatchesBefore := lastTask.DispatchCount
	lastAttempt := store.WorkflowTaskAttempt{TeamID: teamID, WorkflowID: workflow.ID, TaskID: lastID, DispatchToken: lastToken, PlanRevision: lastTask.PlanRevision, WorkflowStep: "last"}
	if tr, err := teamStore.AcceptWorkflowTaskAttempt(ctx, lastAttempt, time.Now().Add(time.Hour)); err != nil || !tr.Applied() {
		t.Fatalf("accept last outcome=%v err=%v", tr.Outcome, err)
	}

	tr, err := teamStore.RequeueWorkflowTaskAttempt(ctx, lastAttempt, "http2: timeout awaiting response headers")
	if err != nil || !tr.Applied() || tr.TaskStatus != store.TeamTaskStatusPending {
		t.Fatalf("requeue transition=%+v err=%v", tr, err)
	}
	if tr.WorkflowStatus != store.TeamWorkflowStatusRunning {
		t.Fatalf("workflow status = %q, want running: a transient failure must not fail the workflow", tr.WorkflowStatus)
	}

	requeued, err := teamStore.GetTask(ctx, lastID)
	if err != nil {
		t.Fatal(err)
	}
	if requeued.Status != store.TeamTaskStatusPending {
		t.Fatalf("task status = %q, want pending so the dispatcher retries it", requeued.Status)
	}
	if requeued.DispatchToken != nil || requeued.DispatchLeaseUntil != nil {
		t.Fatalf("requeue must clear the attempt token: %+v", requeued)
	}
	if requeued.DispatchCount != dispatchesBefore {
		t.Fatalf("DispatchCount = %d, want %d preserved so maxTaskDispatches still bounds retries", requeued.DispatchCount, dispatchesBefore)
	}
	if requeued.OwnerAgentID == nil || *requeued.OwnerAgentID != workerID {
		t.Fatalf("requeue must preserve the owner, got %v", requeued.OwnerAgentID)
	}

	// The already-completed step is untouched — this is the work that used to be lost.
	if done, err := teamStore.GetTask(ctx, doneID); err != nil || done.Status != store.TeamTaskStatusCompleted {
		t.Fatalf("completed step must survive a transient failure downstream: status=%v err=%v", done.Status, err)
	}
	wf, err := teamStore.GetWorkflow(ctx, workflow.ID)
	if err != nil || wf.Status != store.TeamWorkflowStatusRunning {
		t.Fatalf("workflow status=%v err=%v, want still running", wf.Status, err)
	}
	if wf.FailureSummary != "" {
		t.Fatalf("requeue must not record a failure summary, got %q", wf.FailureSummary)
	}

	// The superseded attempt cannot settle the task after requeue.
	if late, err := teamStore.CompleteWorkflowTaskAttempt(ctx, lastAttempt, "late"); err != nil || !late.Stale() {
		t.Fatalf("stale complete after requeue outcome=%v err=%v", late.Outcome, err)
	}
	// Requeueing a task that is not in_progress is a no-op, not a corruption.
	if again, err := teamStore.RequeueWorkflowTaskAttempt(ctx, lastAttempt, "again"); err != nil || again.Applied() {
		t.Fatalf("requeue of non-running attempt outcome=%v err=%v, want non-applied", again.Outcome, err)
	}
}

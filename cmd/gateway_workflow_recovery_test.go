//go:build sqlite || sqliteonly

package cmd

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/agent"
	"github.com/nextlevelbuilder/goclaw/internal/scheduler"
	"github.com/nextlevelbuilder/goclaw/internal/store"
	"github.com/nextlevelbuilder/goclaw/internal/store/sqlitestore"
	"github.com/nextlevelbuilder/goclaw/internal/tasks"
)

type recoveryTickerFixture struct {
	teamStore *sqlitestore.SQLiteTeamStore
	ctx       context.Context
	teamID    uuid.UUID
	workflow  *store.TeamWorkflowData
	taskID    uuid.UUID
}

func newRecoveryTickerFixture(t *testing.T) recoveryTickerFixture {
	t.Helper()

	db, err := sqlitestore.OpenDB(filepath.Join(t.TempDir(), "recovery-ticker.db"))
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := sqlitestore.EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}

	tenantID, leadID, workerID, teamID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := db.Exec(`INSERT INTO tenants(id,name,slug,status,settings) VALUES(?,?,?,'active','{}')`, tenantID, "Recovery tenant", "recovery-"+tenantID.String()); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	for id, key := range map[uuid.UUID]string{leadID: "recovery-lead", workerID: "recovery-worker"} {
		if _, err := db.Exec(`INSERT INTO agents(id,agent_key,owner_id,provider,model,tenant_id) VALUES(?,?,?,'openai','test',?)`, id, key, "owner", tenantID); err != nil {
			t.Fatalf("insert agent %q: %v", key, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO agent_teams(id,name,lead_agent_id,status,settings,created_by,tenant_id) VALUES(?,?,?,'active','{"version":2}','owner',?)`, teamID, "Recovery team", leadID, tenantID); err != nil {
		t.Fatalf("insert team: %v", err)
	}
	for agentID, role := range map[uuid.UUID]string{leadID: "lead", workerID: "member"} {
		if _, err := db.Exec(`INSERT INTO agent_team_members(team_id,agent_id,role,tenant_id) VALUES(?,?,?,?)`, teamID, agentID, role, tenantID); err != nil {
			t.Fatalf("insert team member: %v", err)
		}
	}

	ctx := store.WithTenantID(context.Background(), tenantID)
	teamStore := sqlitestore.NewSQLiteTeamStore(db)
	plan := []byte(`{"schema_version":1,"goal":"recover blocked work","steps":[]}`)
	workflow := &store.TeamWorkflowData{
		TeamID:              teamID,
		Status:              store.TeamWorkflowStatusRunning,
		CanonicalPlan:       plan,
		SchemaVersion:       1,
		PlanHash:            fmt.Sprintf("%x", sha256.Sum256(plan)),
		CoordinatorAgentID:  leadID,
		CoordinatorAgentKey: "recovery-lead",
		OriginAgentID:       leadID,
		OriginAgentKey:      "recovery-lead",
		OriginRunID:         "recovery-ticker-run",
		OriginSessionKey:    "agent:recovery-lead:ws:direct:user",
		OriginChannel:       "ws",
		OriginChatID:        "user",
	}
	taskID := uuid.New()
	if err := teamStore.CreateWorkflowWithTasks(ctx, workflow, []store.TeamTaskData{{
		BaseModel:        store.BaseModel{ID: taskID},
		TeamID:           teamID,
		Subject:          "Blocked work",
		Status:           store.TeamTaskStatusPending,
		OwnerAgentID:     &workerID,
		WorkflowStepID:   "blocked-work",
		WorkflowKind:     store.TeamWorkflowTaskKindWork,
		WorkflowTerminal: true,
	}}); err != nil {
		t.Fatalf("CreateWorkflowWithTasks: %v", err)
	}

	token, err := teamStore.ClaimWorkflowTaskDispatch(ctx, taskID, teamID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatalf("ClaimWorkflowTaskDispatch: %v", err)
	}
	task, err := teamStore.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask before block: %v", err)
	}
	attempt := store.WorkflowTaskAttempt{
		TeamID:        teamID,
		WorkflowID:    workflow.ID,
		TaskID:        taskID,
		DispatchToken: token,
		PlanRevision:  task.PlanRevision,
		WorkflowStep:  task.WorkflowStepID,
	}
	if transition, err := teamStore.AcceptWorkflowTaskAttempt(ctx, attempt, time.Now().Add(time.Hour)); err != nil || !transition.Applied() {
		t.Fatalf("AcceptWorkflowTaskAttempt transition=%+v err=%v", transition, err)
	}
	if transition, err := teamStore.BlockWorkflowTaskAttempt(ctx, attempt, "needs coordinator decision"); err != nil || !transition.Applied() {
		t.Fatalf("BlockWorkflowTaskAttempt transition=%+v err=%v", transition, err)
	}

	return recoveryTickerFixture{teamStore: teamStore, ctx: ctx, teamID: teamID, workflow: workflow, taskID: taskID}
}

func startRecoveryTicker(t *testing.T, fixture recoveryTickerFixture, sched *scheduler.Scheduler, completed chan<- struct{}) *tasks.TaskTicker {
	t.Helper()

	ticker := tasks.NewTaskTicker(fixture.teamStore, nil, nil, 3600)
	ticker.SetWorkflowRuntime(nil, nil, func(recoveryCtx context.Context, claim store.EscalationClaim) {
		go func() {
			recoverWorkflowBlocker(workflowBackgroundContext(recoveryCtx), &ConsumerDeps{
				Sched:     sched,
				TeamStore: fixture.teamStore,
			}, fixture.teamStore, claim.WorkflowID, claim.TaskID)
			completed <- struct{}{}
		}()
	})
	ticker.Start()
	t.Cleanup(ticker.Stop)
	return ticker
}

func waitForRecovery(t *testing.T, completed <-chan struct{}) {
	t.Helper()
	select {
	case <-completed:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for recovery ticker callback")
	}
}

func assertEscalationReclaimable(t *testing.T, fixture recoveryTickerFixture, wantAttempt int) *store.TeamTaskData {
	t.Helper()

	task, err := fixture.teamStore.GetTask(fixture.ctx, fixture.taskID)
	if err != nil {
		t.Fatalf("GetTask after recovery: %v", err)
	}
	if task.Status != store.TeamTaskStatusBlocked || task.EscalationStatus != store.TeamTaskEscalationEnqueuing || task.EscalationAttemptCount != wantAttempt || task.EscalationNextAt == nil {
		t.Fatalf("persisted escalation = %+v, want blocked/enqueuing attempt=%d with next_at", task, wantAttempt)
	}
	if due, err := fixture.teamStore.ListEscalationDueTasks(store.WithCrossTenant(fixture.ctx), task.EscalationNextAt.Add(time.Nanosecond)); err != nil || !containsRecoveryTask(due, fixture.taskID) {
		t.Fatalf("next retry must be visible to list-due: due=%d err=%v", len(due), err)
	}
	claim, err := fixture.teamStore.ClaimTaskEscalation(fixture.ctx, fixture.taskID, fixture.teamID, task.EscalationNextAt.Add(time.Nanosecond))
	if err != nil || !claim.Claimed || claim.Attempt != wantAttempt+1 {
		t.Fatalf("next retry claim = %+v err=%v, want claimed attempt=%d", claim, err, wantAttempt+1)
	}
	return task
}

func containsRecoveryTask(tasks []store.TeamTaskData, taskID uuid.UUID) bool {
	for _, task := range tasks {
		if task.ID == taskID {
			return true
		}
	}
	return false
}

// TestWorkflowRecoveryTicker_RunErrorLeavesEscalationReclaimable drives the
// production ticker seam (list-due -> claim -> callback -> scheduler outcome).
// A recovery RunOutcome.Err must not mark the durable escalation delivered.
func TestWorkflowRecoveryTicker_RunErrorLeavesEscalationReclaimable(t *testing.T) {
	fixture := newRecoveryTickerFixture(t)
	runStarted := make(chan struct{}, 1)
	sched := scheduler.NewScheduler(nil, scheduler.QueueConfig{Mode: scheduler.QueueModeQueue, Cap: 4, MaxConcurrent: 1}, func(_ context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		if req.RunKind != agent.RunKindWorkflowRecovery || req.WorkflowRecovery == nil || req.WorkflowRecovery.BlockedTaskID != fixture.taskID {
			return nil, fmt.Errorf("unexpected recovery request: %+v", req)
		}
		runStarted <- struct{}{}
		return nil, errors.New("coordinator run failed")
	})
	completed := make(chan struct{}, 1)
	startRecoveryTicker(t, fixture, sched, completed)

	waitForRecovery(t, completed)
	select {
	case <-runStarted:
	case <-time.After(time.Second):
		t.Fatal("recovery ticker did not schedule the coordinator run")
	}
	assertEscalationReclaimable(t, fixture, 1)
}

// TestWorkflowRecoveryTicker_ScheduleRejectionLeavesEscalationReclaimable
// exercises the real scheduler rejection path. The ticker's callback still
// returns after Schedule reports ErrGatewayDraining, and the persisted retry is
// surfaced by the next list-due sweep and can be claimed again.
func TestWorkflowRecoveryTicker_ScheduleRejectionLeavesEscalationReclaimable(t *testing.T) {
	fixture := newRecoveryTickerFixture(t)
	runStarted := make(chan struct{}, 1)
	sched := scheduler.NewScheduler(nil, scheduler.QueueConfig{Mode: scheduler.QueueModeQueue, Cap: 4, MaxConcurrent: 1}, func(context.Context, agent.RunRequest) (*agent.RunResult, error) {
		runStarted <- struct{}{}
		return nil, nil
	})
	sched.MarkDraining()
	completed := make(chan struct{}, 1)
	startRecoveryTicker(t, fixture, sched, completed)

	waitForRecovery(t, completed)
	select {
	case <-runStarted:
		t.Fatal("draining scheduler unexpectedly ran recovery")
	default:
	}
	assertEscalationReclaimable(t, fixture, 1)
}

// TestWorkflowRecoveryTicker_CoordinatorResolutionClearsEscalation is the
// success control: completing the scheduler run alone is not the acknowledgement;
// the coordinator's real retry action is what clears the escalation ledger.
func TestWorkflowRecoveryTicker_CoordinatorResolutionClearsEscalation(t *testing.T) {
	fixture := newRecoveryTickerFixture(t)
	runResolved := make(chan struct{}, 1)
	sched := scheduler.NewScheduler(nil, scheduler.QueueConfig{Mode: scheduler.QueueModeQueue, Cap: 4, MaxConcurrent: 1}, func(ctx context.Context, req agent.RunRequest) (*agent.RunResult, error) {
		if req.WorkflowRecovery == nil {
			return nil, errors.New("missing recovery context")
		}
		transition, err := fixture.teamStore.RetryBlockedWorkflowTask(ctx, req.WorkflowRecovery.BlockedTaskID, req.WorkflowRecovery.TeamID, "retry with the coordinator decision")
		if err != nil {
			return nil, err
		}
		if !transition.Applied() {
			return nil, fmt.Errorf("recovery action was not applied: %s", transition.Outcome)
		}
		runResolved <- struct{}{}
		return &agent.RunResult{}, nil
	})
	completed := make(chan struct{}, 1)
	startRecoveryTicker(t, fixture, sched, completed)

	waitForRecovery(t, completed)
	select {
	case <-runResolved:
	case <-time.After(time.Second):
		t.Fatal("coordinator recovery action did not run")
	}

	task, err := fixture.teamStore.GetTask(fixture.ctx, fixture.taskID)
	if err != nil {
		t.Fatalf("GetTask after coordinator resolution: %v", err)
	}
	if task.Status != store.TeamTaskStatusPending || task.EscalationStatus != store.TeamTaskEscalationDelivered || task.EscalationNextAt != nil || task.EscalationAttemptCount != 0 {
		t.Fatalf("coordinator resolution must clear escalation: %+v", task)
	}
	if due, err := fixture.teamStore.ListEscalationDueTasks(store.WithCrossTenant(fixture.ctx), time.Now().Add(time.Hour)); err != nil || containsRecoveryTask(due, fixture.taskID) {
		t.Fatalf("resolved escalation must not be listed due: due=%d err=%v", len(due), err)
	}
}

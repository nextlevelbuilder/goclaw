//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func sqliteAdminActionGuard(action store.WorkflowAction, teamID, workflowID uuid.UUID, status string, revision int, reason string) store.WorkflowActionGuard {
	return store.WorkflowActionGuard{
		Action:               action,
		TeamID:               teamID,
		WorkflowID:           workflowID,
		ExpectedStatus:       status,
		ExpectedPlanRevision: revision,
		Reason:               reason,
		Actor: store.WorkflowActionActor{
			Kind:   store.WorkflowActorAdmin,
			UserID: "operator",
		},
	}
}

func sqliteStepActionGuard(action store.WorkflowAction, teamID, workflowID, taskID uuid.UUID, workflowStatus, taskStatus, reason string) store.WorkflowActionGuard {
	guard := sqliteAdminActionGuard(action, teamID, workflowID, workflowStatus, 1, reason)
	guard.TaskID = &taskID
	guard.ExpectedTaskStatus = taskStatus
	return guard
}

func TestSQLiteApplyWorkflowActionTransitions(t *testing.T) {
	t.Run("retry blocked", func(t *testing.T) {
		teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
		blockedID := uuid.New()
		workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{{
			BaseModel: store.BaseModel{ID: blockedID}, TeamID: teamID, Subject: "Blocked", Status: store.TeamTaskStatusPending,
			OwnerAgentID: &workerID, WorkflowStepID: "blocked", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true,
		}})
		if _, err := teamStore.db.Exec(`UPDATE team_tasks SET status=?,blocker_reason='missing key',escalation_status=?,escalation_attempt_count=2,escalation_next_at=? WHERE id=?`,
			store.TeamTaskStatusBlocked, store.TeamTaskEscalationPending, time.Now(), blockedID); err != nil {
			t.Fatal(err)
		}
		guard := sqliteStepActionGuard(store.WorkflowActionRetryBlocked, teamID, workflow.ID, blockedID, store.TeamWorkflowStatusRunning, store.TeamTaskStatusBlocked, "use staging key")
		result, err := teamStore.ApplyWorkflowAction(ctx, guard)
		if err != nil || result.Outcome != store.WorkflowActionApplied {
			t.Fatalf("ApplyWorkflowAction() result=%+v error=%v", result, err)
		}
		task, err := teamStore.GetTask(ctx, blockedID)
		if err != nil || task.Status != store.TeamTaskStatusPending || task.BlockerReason != "" || task.RecoveryCount != 1 || task.EscalationStatus != store.TeamTaskEscalationDelivered || task.EscalationAttemptCount != 0 {
			t.Fatalf("retried task=%+v error=%v", task, err)
		}
		comments, err := teamStore.ListTaskComments(ctx, blockedID)
		if err != nil || len(comments) != 1 || comments[0].UserID != "operator" || comments[0].AgentID != nil || comments[0].Content != "use staging key" {
			t.Fatalf("comments=%+v error=%v", comments, err)
		}
		replay, err := teamStore.ApplyWorkflowAction(ctx, guard)
		if err != nil || replay.Outcome != store.WorkflowActionAlreadyApplied {
			t.Fatalf("replay result=%+v error=%v", replay, err)
		}
		comments, _ = teamStore.ListTaskComments(ctx, blockedID)
		if len(comments) != 1 {
			t.Fatalf("replay persisted %d comments, want 1", len(comments))
		}
	})

	t.Run("cancel workflow", func(t *testing.T) {
		teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
		doneID, terminalID := uuid.New(), uuid.New()
		workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
			{BaseModel: store.BaseModel{ID: doneID}, TeamID: teamID, Subject: "Done", Status: store.TeamTaskStatusCompleted, Result: sqliteRecoveryStrPtr("kept"), OwnerAgentID: &workerID, WorkflowStepID: "done", WorkflowKind: store.TeamWorkflowTaskKindWork},
			{BaseModel: store.BaseModel{ID: terminalID}, TeamID: teamID, Subject: "Terminal", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "terminal", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
		})
		guard := sqliteAdminActionGuard(store.WorkflowActionCancelWorkflow, teamID, workflow.ID, store.TeamWorkflowStatusRunning, 1, "operator cancelled")
		result, err := teamStore.ApplyWorkflowAction(ctx, guard)
		if err != nil || result.Outcome != store.WorkflowActionApplied || result.Workflow.Status != store.TeamWorkflowStatusCancelling || result.Workflow.CancelReason != "operator cancelled" {
			t.Fatalf("cancel result=%+v error=%v", result, err)
		}
		done, _ := teamStore.GetTask(ctx, doneID)
		terminal, _ := teamStore.GetTask(ctx, terminalID)
		if done.Status != store.TeamTaskStatusCompleted || done.Result == nil || *done.Result != "kept" || terminal.Status != store.TeamTaskStatusCancelled {
			t.Fatalf("cancelled tasks done=%+v terminal=%+v", done, terminal)
		}
		replay, err := teamStore.ApplyWorkflowAction(ctx, guard)
		if err != nil || replay.Outcome != store.WorkflowActionAlreadyApplied {
			t.Fatalf("cancel replay=%+v error=%v", replay, err)
		}
	})

	t.Run("fail invalidates finalizer", func(t *testing.T) {
		teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
		terminalID := uuid.New()
		workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{{
			BaseModel: store.BaseModel{ID: terminalID}, TeamID: teamID, Subject: "Done", Status: store.TeamTaskStatusCompleted,
			Result: sqliteRecoveryStrPtr("evidence"), OwnerAgentID: &workerID, WorkflowStepID: "terminal", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true,
		}})
		_, staleFinalizeToken, err := teamStore.ClaimWorkflowFinalization(ctx, workflow.ID, time.Now().Add(time.Minute))
		if err != nil {
			t.Fatalf("claim finalization: %v", err)
		}
		guard := sqliteAdminActionGuard(store.WorkflowActionFailWorkflow, teamID, workflow.ID, store.TeamWorkflowStatusRunning, 1, "cannot deliver safely")
		result, err := teamStore.ApplyWorkflowAction(ctx, guard)
		if err != nil || result.Outcome != store.WorkflowActionApplied || result.Workflow.Status != store.TeamWorkflowStatusFailing || result.Workflow.FinalizeToken != nil || result.Workflow.FinalizeLeaseUntil != nil {
			t.Fatalf("fail result=%+v error=%v", result, err)
		}
		if err := teamStore.CompleteWorkflowFinalization(ctx, workflow.ID, staleFinalizeToken, store.TeamWorkflowStatusCompleted, "stale result"); err == nil {
			t.Fatal("pre-action finalize token remained valid")
		}
		replay, err := teamStore.ApplyWorkflowAction(ctx, guard)
		if err != nil || replay.Outcome != store.WorkflowActionAlreadyApplied {
			t.Fatalf("fail replay=%+v error=%v", replay, err)
		}
	})

	t.Run("retry expansion", func(t *testing.T) {
		teamStore, ctx, leadID, _, teamID := sqliteRecoveryFixture(t)
		plan := []byte(`{"schema_version":1,"goal":"expand","steps":[]}`)
		workflow := &store.TeamWorkflowData{
			TeamID: teamID, Status: store.TeamWorkflowStatusPendingExpansion, CanonicalPlan: plan, SchemaVersion: 1,
			PlanHash: fmt.Sprintf("%x", sha256.Sum256(plan)), CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead",
			OriginAgentID: leadID, OriginAgentKey: "lead", OriginRunID: "manual-expand", OriginSessionKey: "session", OriginChannel: "ws", OriginChatID: "user",
		}
		audit := &store.TeamTaskData{TeamID: teamID, Subject: "Audit", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID}
		if err := teamStore.CreatePendingWorkflowRequest(ctx, workflow, audit); err != nil {
			t.Fatal(err)
		}
		if _, err := teamStore.db.Exec(`UPDATE team_workflows SET expansion_attempt_count=2,last_expansion_error='timeout',expansion_token=?,expansion_lease_until=?,next_expansion_at=? WHERE id=?`, uuid.New(), time.Now().Add(-time.Hour), time.Now().Add(time.Hour), workflow.ID); err != nil {
			t.Fatal(err)
		}
		guard := sqliteAdminActionGuard(store.WorkflowActionRetryExpansion, teamID, workflow.ID, store.TeamWorkflowStatusPendingExpansion, 1, "retry with restored provider")
		result, err := teamStore.ApplyWorkflowAction(ctx, guard)
		if err != nil || result.Outcome != store.WorkflowActionApplied || result.Workflow.ExpansionAttemptCount != 2 || result.Workflow.LastExpansionError != "timeout" || result.Workflow.ExpansionToken != nil || result.Workflow.NextExpansionAt == nil || result.Workflow.NextExpansionAt.After(time.Now()) {
			t.Fatalf("retry expansion result=%+v error=%v", result, err)
		}
		replay, err := teamStore.ApplyWorkflowAction(ctx, guard)
		if err != nil || replay.Outcome != store.WorkflowActionAlreadyApplied {
			t.Fatalf("retry expansion replay=%+v error=%v", replay, err)
		}
	})

	t.Run("retry delivery", func(t *testing.T) {
		teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
		terminalID := uuid.New()
		workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{{
			BaseModel: store.BaseModel{ID: terminalID}, TeamID: teamID, Subject: "Done", Status: store.TeamTaskStatusCompleted,
			Result: sqliteRecoveryStrPtr("final"), OwnerAgentID: &workerID, WorkflowStepID: "terminal", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true,
		}})
		_, finalizeToken, err := teamStore.ClaimWorkflowFinalization(ctx, workflow.ID, time.Now().Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if err := teamStore.CompleteWorkflowFinalization(ctx, workflow.ID, finalizeToken, store.TeamWorkflowStatusCompleted, "final"); err != nil {
			t.Fatal(err)
		}
		if _, err := teamStore.db.Exec(`UPDATE team_workflows SET delivery_status=?,delivery_attempt_count=?,last_delivery_error='dead channel',next_delivery_at=NULL WHERE id=?`, store.TeamWorkflowDeliveryDead, store.MaxWorkflowDeliveryAttempts, workflow.ID); err != nil {
			t.Fatal(err)
		}
		if _, _, err := teamStore.ClaimWorkflowDelivery(ctx, workflow.ID, time.Now().Add(time.Minute)); err == nil {
			t.Fatal("dead delivery was claimed without retry_delivery")
		}
		guard := sqliteAdminActionGuard(store.WorkflowActionRetryDelivery, teamID, workflow.ID, store.TeamWorkflowStatusCompleted, 1, "channel restored")
		result, err := teamStore.ApplyWorkflowAction(ctx, guard)
		if err != nil || result.Outcome != store.WorkflowActionApplied || result.Workflow.DeliveryStatus != store.TeamWorkflowDeliveryPending || result.Workflow.DeliveryAttemptCount != 0 || result.Workflow.LastDeliveryError != "dead channel" {
			t.Fatalf("retry delivery result=%+v error=%v", result, err)
		}
		if _, _, err := teamStore.ClaimWorkflowDelivery(ctx, workflow.ID, time.Now().Add(time.Minute)); err != nil {
			t.Fatalf("re-armed delivery was not claimable: %v", err)
		}
	})
}

func TestSQLiteApplyWorkflowActionCoordinatorAttributionAndCommentRollback(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
	blockedID := uuid.New()
	workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{{
		BaseModel: store.BaseModel{ID: blockedID}, TeamID: teamID, Subject: "Blocked", Status: store.TeamTaskStatusPending,
		OwnerAgentID: &workerID, WorkflowStepID: "blocked", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true,
	}})
	if _, err := teamStore.db.Exec(`UPDATE team_tasks SET status=?,blocker_reason='blocked' WHERE id=?`, store.TeamTaskStatusBlocked, blockedID); err != nil {
		t.Fatal(err)
	}

	missingAgentID := uuid.New()
	guard := sqliteStepActionGuard(store.WorkflowActionRetryBlocked, teamID, workflow.ID, blockedID, store.TeamWorkflowStatusRunning, store.TeamTaskStatusBlocked, "retry as coordinator")
	guard.Actor = store.WorkflowActionActor{Kind: store.WorkflowActorCoordinator, AgentID: &missingAgentID}
	if result, err := teamStore.ApplyWorkflowAction(ctx, guard); err == nil {
		t.Fatalf("missing coordinator FK result=%+v, want error", result)
	}
	task, err := teamStore.GetTask(ctx, blockedID)
	if err != nil || task.Status != store.TeamTaskStatusBlocked || task.BlockerReason != "blocked" || task.RecoveryCount != 0 || task.CommentCount != 0 {
		t.Fatalf("late comment error did not roll back task: task=%+v error=%v", task, err)
	}
	current, err := teamStore.GetWorkflow(ctx, workflow.ID)
	if err != nil || current.Status != store.TeamWorkflowStatusRunning {
		t.Fatalf("late comment error changed workflow: workflow=%+v error=%v", current, err)
	}
	if comments, err := teamStore.ListTaskComments(ctx, blockedID); err != nil || len(comments) != 0 {
		t.Fatalf("late comment error persisted comments=%+v error=%v", comments, err)
	}

	guard.Actor.AgentID = &leadID
	result, err := teamStore.ApplyWorkflowAction(ctx, guard)
	if err != nil || result.Outcome != store.WorkflowActionApplied {
		t.Fatalf("coordinator action result=%+v error=%v", result, err)
	}
	comments, err := teamStore.ListTaskComments(ctx, blockedID)
	if err != nil || len(comments) != 1 || comments[0].AgentID == nil || *comments[0].AgentID != leadID || comments[0].UserID != "" {
		t.Fatalf("coordinator comment attribution=%+v error=%v", comments, err)
	}
}

func TestSQLiteApplyWorkflowActionGuardsAndConcurrency(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
	blockedID := uuid.New()
	workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{{
		BaseModel: store.BaseModel{ID: blockedID}, TeamID: teamID, Subject: "Blocked", Status: store.TeamTaskStatusPending,
		OwnerAgentID: &workerID, WorkflowStepID: "blocked", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true,
	}})
	if _, err := teamStore.db.Exec(`UPDATE team_tasks SET status=?,blocker_reason='blocked' WHERE id=?`, store.TeamTaskStatusBlocked, blockedID); err != nil {
		t.Fatal(err)
	}
	guard := sqliteStepActionGuard(store.WorkflowActionRetryBlocked, teamID, workflow.ID, blockedID, store.TeamWorkflowStatusRunning, store.TeamTaskStatusBlocked, "retry once")

	stale := guard
	stale.ExpectedPlanRevision = 2
	if result, err := teamStore.ApplyWorkflowAction(ctx, stale); err != nil || result.Outcome != store.WorkflowActionConflict {
		t.Fatalf("stale revision result=%+v error=%v", result, err)
	}
	wrongTask := guard
	otherID := uuid.New()
	wrongTask.TaskID = &otherID
	if result, err := teamStore.ApplyWorkflowAction(ctx, wrongTask); err != nil || result.Outcome != store.WorkflowActionConflict {
		t.Fatalf("wrong task result=%+v error=%v", result, err)
	}
	if comments, _ := teamStore.ListTaskComments(ctx, blockedID); len(comments) != 0 {
		t.Fatalf("conflicts persisted %d comments", len(comments))
	}

	const callers = 8
	results := make(chan store.WorkflowActionResult, callers)
	errs := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() {
			result, err := teamStore.ApplyWorkflowAction(ctx, guard)
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	applied, already := 0, 0
	for i := 0; i < callers; i++ {
		select {
		case err := <-errs:
			t.Fatalf("concurrent action error: %v", err)
		case result := <-results:
			switch result.Outcome {
			case store.WorkflowActionApplied:
				applied++
			case store.WorkflowActionAlreadyApplied:
				already++
			default:
				t.Fatalf("unexpected concurrent outcome %v", result.Outcome)
			}
		}
	}
	if applied != 1 || already != callers-1 {
		t.Fatalf("concurrent outcomes applied=%d already=%d", applied, already)
	}
	if comments, _ := teamStore.ListTaskComments(ctx, blockedID); len(comments) != 1 {
		t.Fatalf("concurrent action persisted %d comments, want 1", len(comments))
	}
}

func TestSQLiteWorkflowActionReplayConcurrencyMatrix(t *testing.T) {
	const callers = 6
	for _, action := range store.AllWorkflowActions {
		t.Run(string(action), func(t *testing.T) {
			teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
			workflowID, commentTaskID, guard := sqliteActionMatrixFixture(t, teamStore, ctx, leadID, workerID, teamID, action)
			for _, bad := range sqliteActionMatrixBadGuards(ctx, guard, action) {
				result, err := teamStore.ApplyWorkflowAction(bad.ctx, bad.guard)
				if err != nil && err != store.ErrTaskNotFound {
					t.Fatalf("%s guard error=%v", bad.name, err)
				}
				if err == nil && result.Outcome != store.WorkflowActionConflict {
					t.Fatalf("%s guard result=%+v, want Conflict", bad.name, result)
				}
			}
			start := make(chan struct{})
			results := make(chan store.WorkflowActionResult, callers)
			errs := make(chan error, callers)
			var wg sync.WaitGroup
			for i := 0; i < callers; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					<-start
					result, err := teamStore.ApplyWorkflowAction(ctx, guard)
					if err != nil {
						errs <- err
						return
					}
					results <- result
				}()
			}
			close(start)
			wg.Wait()
			close(results)
			close(errs)
			applied, replay := 0, 0
			for err := range errs {
				t.Fatalf("concurrent %s error: %v", action, err)
			}
			for result := range results {
				switch result.Outcome {
				case store.WorkflowActionApplied:
					applied++
				case store.WorkflowActionAlreadyApplied, store.WorkflowActionConflict:
					replay++
				default:
					t.Fatalf("concurrent %s outcome=%v", action, result.Outcome)
				}
			}
			if applied != 1 || replay != callers-1 {
				t.Fatalf("concurrent %s applied=%d replay/conflict=%d", action, applied, replay)
			}
			replayResult, err := teamStore.ApplyWorkflowAction(ctx, guard)
			if err != nil || replayResult.Outcome != store.WorkflowActionAlreadyApplied {
				t.Fatalf("post-state %s result=%+v error=%v, want %v", action, replayResult, err, store.WorkflowActionAlreadyApplied)
			}
			comments, err := teamStore.ListTaskComments(ctx, commentTaskID)
			if err != nil || len(comments) != 1 {
				t.Fatalf("%s comments=%d error=%v, want 1", action, len(comments), err)
			}
			workflow, err := teamStore.GetWorkflow(ctx, workflowID)
			if err != nil {
				t.Fatal(err)
			}
			if action == store.WorkflowActionRetryExpansion && workflow.NextExpansionAt == nil {
				t.Fatal("retry_expansion did not re-arm")
			}
			if action == store.WorkflowActionRetryDelivery && workflow.DeliveryStatus != store.TeamWorkflowDeliveryPending {
				t.Fatalf("retry_delivery status=%q", workflow.DeliveryStatus)
			}
		})
	}
}

type sqliteMatrixBadGuard struct {
	name  string
	ctx   context.Context
	guard store.WorkflowActionGuard
}

func sqliteActionMatrixBadGuards(ctx context.Context, guard store.WorkflowActionGuard, action store.WorkflowAction) []sqliteMatrixBadGuard {
	guards := []sqliteMatrixBadGuard{{"wrong tenant", store.WithTenantID(context.Background(), uuid.New()), guard}, {"wrong team", ctx, guard}, {"wrong status", ctx, guard}, {"wrong revision", ctx, guard}}
	guards[1].guard.TeamID = uuid.New()
	guards[2].guard.ExpectedStatus = "wrong-status"
	guards[3].guard.ExpectedPlanRevision++
	if action.StepScoped() {
		bad := guard
		id := uuid.New()
		bad.TaskID = &id
		guards = append(guards, sqliteMatrixBadGuard{"wrong task", ctx, bad})
	}
	return guards
}

func sqliteActionMatrixFixture(t *testing.T, teamStore *SQLiteTeamStore, ctx context.Context, leadID, workerID, teamID uuid.UUID, action store.WorkflowAction) (uuid.UUID, uuid.UUID, store.WorkflowActionGuard) {
	t.Helper()
	if action == store.WorkflowActionRetryExpansion {
		plan := []byte(`{"schema_version":1,"goal":"expand","steps":[]}`)
		workflow := &store.TeamWorkflowData{TeamID: teamID, Status: store.TeamWorkflowStatusPendingExpansion, CanonicalPlan: plan, SchemaVersion: 1, PlanHash: fmt.Sprintf("%x", sha256.Sum256(plan)), CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead", OriginAgentID: leadID, OriginAgentKey: "lead", OriginRunID: "matrix-expand-" + uuid.NewString(), OriginSessionKey: "session", OriginChannel: "ws", OriginChatID: "user"}
		audit := &store.TeamTaskData{TeamID: teamID, Subject: "Audit", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID}
		if err := teamStore.CreatePendingWorkflowRequest(ctx, workflow, audit); err != nil {
			t.Fatal(err)
		}
		if _, err := teamStore.db.Exec(`UPDATE team_workflows SET expansion_token=?,expansion_lease_until=?,next_expansion_at=? WHERE id=?`, uuid.New(), time.Now().Add(-time.Hour), time.Now().Add(time.Hour), workflow.ID); err != nil {
			t.Fatal(err)
		}
		return workflow.ID, audit.ID, sqliteAdminActionGuard(action, teamID, workflow.ID, workflow.Status, 1, "matrix retry expansion")
	}
	taskID := uuid.New()
	taskStatus := store.TeamTaskStatusPending
	if action == store.WorkflowActionRetryDelivery {
		taskStatus = store.TeamTaskStatusCompleted
	}
	workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{{BaseModel: store.BaseModel{ID: taskID}, TeamID: teamID, Subject: "Matrix", Status: taskStatus, OwnerAgentID: &workerID, WorkflowStepID: "step", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true}})
	if action == store.WorkflowActionRetryDelivery {
		_, token, err := teamStore.ClaimWorkflowFinalization(ctx, workflow.ID, time.Now().Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if err := teamStore.CompleteWorkflowFinalization(ctx, workflow.ID, token, store.TeamWorkflowStatusCompleted, "done"); err != nil {
			t.Fatal(err)
		}
		if _, err := teamStore.db.Exec(`UPDATE team_workflows SET delivery_status=?,next_delivery_at=NULL WHERE id=?`, store.TeamWorkflowDeliveryDead, workflow.ID); err != nil {
			t.Fatal(err)
		}
		return workflow.ID, taskID, sqliteAdminActionGuard(action, teamID, workflow.ID, store.TeamWorkflowStatusCompleted, 1, "matrix retry delivery")
	}
	if _, err := teamStore.db.Exec(`UPDATE team_tasks SET status=?,blocker_reason='blocked' WHERE id=?`, store.TeamTaskStatusBlocked, taskID); err != nil {
		t.Fatal(err)
	}
	switch action {
	case store.WorkflowActionRetryBlocked:
		return workflow.ID, taskID, sqliteStepActionGuard(action, teamID, workflow.ID, taskID, store.TeamWorkflowStatusRunning, store.TeamTaskStatusBlocked, "matrix retry")
	case store.WorkflowActionCancelWorkflow:
		return workflow.ID, taskID, sqliteAdminActionGuard(action, teamID, workflow.ID, store.TeamWorkflowStatusRunning, 1, "matrix cancel")
	case store.WorkflowActionFailWorkflow:
		return workflow.ID, taskID, sqliteAdminActionGuard(action, teamID, workflow.ID, store.TeamWorkflowStatusRunning, 1, "matrix fail")
	}
	t.Fatalf("unhandled action %s", action)
	return uuid.Nil, uuid.Nil, store.WorkflowActionGuard{}
}

func TestSQLiteWorkflowCreationRejectsTaskScopeMismatch(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
	otherTeamID := uuid.New()
	otherTenantID := uuid.New()
	plan := []byte(`{"schema_version":1,"goal":"scope","steps":[]}`)
	newWorkflow := func(runID string) *store.TeamWorkflowData {
		return &store.TeamWorkflowData{
			TeamID: teamID, Status: store.TeamWorkflowStatusRunning, CanonicalPlan: plan, SchemaVersion: 1,
			PlanHash: fmt.Sprintf("%x", sha256.Sum256(plan)), CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead",
			OriginAgentID: leadID, OriginAgentKey: "lead", OriginRunID: runID, OriginSessionKey: "session", OriginChannel: "ws", OriginChatID: "user",
		}
	}
	for _, test := range []struct {
		name string
		task store.TeamTaskData
	}{
		{name: "team", task: store.TeamTaskData{BaseModel: store.BaseModel{ID: uuid.New()}, TeamID: otherTeamID, Subject: "wrong team", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "one", WorkflowTerminal: true}},
		{name: "tenant", task: store.TeamTaskData{BaseModel: store.BaseModel{ID: uuid.New()}, TeamID: teamID, TenantID: otherTenantID, Subject: "wrong tenant", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "one", WorkflowTerminal: true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			workflow := newWorkflow("scope-" + test.name)
			if err := teamStore.CreateWorkflowWithTasks(ctx, workflow, []store.TeamTaskData{test.task}); err == nil {
				t.Fatal("scope mismatch was accepted")
			}
			var count int
			if err := teamStore.db.QueryRow(`SELECT COUNT(*) FROM team_workflows WHERE id=?`, workflow.ID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("workflow persisted after rejected scope: count=%d error=%v", count, err)
			}
		})
	}
}

func TestSQLiteClaimWorkflowDeliveryHonorsFutureRetry(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := sqliteRecoveryFixture(t)
	workflow := sqliteMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{{
		BaseModel: store.BaseModel{ID: uuid.New()}, TeamID: teamID, Subject: "Done", Status: store.TeamTaskStatusCompleted,
		Result: sqliteRecoveryStrPtr("final"), OwnerAgentID: &workerID, WorkflowStepID: "terminal", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true,
	}})
	_, finalizeToken, err := teamStore.ClaimWorkflowFinalization(ctx, workflow.ID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := teamStore.CompleteWorkflowFinalization(ctx, workflow.ID, finalizeToken, store.TeamWorkflowStatusCompleted, "final"); err != nil {
		t.Fatal(err)
	}
	if _, err := teamStore.db.Exec(`UPDATE team_workflows SET delivery_status=?,next_delivery_at=? WHERE id=?`, store.TeamWorkflowDeliveryPending, time.Now().Add(time.Hour), workflow.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := teamStore.ClaimWorkflowDelivery(ctx, workflow.ID, time.Now().Add(time.Minute)); err == nil {
		t.Fatal("future pending delivery was claimed early")
	}
}

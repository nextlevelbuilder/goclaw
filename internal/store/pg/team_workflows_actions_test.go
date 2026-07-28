package pg

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

func pgAdminActionGuard(action store.WorkflowAction, teamID, workflowID uuid.UUID, status string, revision int, reason string) store.WorkflowActionGuard {
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

func pgStepActionGuard(action store.WorkflowAction, teamID, workflowID, taskID uuid.UUID, workflowStatus, taskStatus, reason string) store.WorkflowActionGuard {
	guard := pgAdminActionGuard(action, teamID, workflowID, workflowStatus, 1, reason)
	guard.TaskID = &taskID
	guard.ExpectedTaskStatus = taskStatus
	return guard
}

func TestPGApplyWorkflowActionTransitions(t *testing.T) {
	t.Run("retry blocked", func(t *testing.T) {
		teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
		blockedID := uuid.New()
		workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{{
			BaseModel: store.BaseModel{ID: blockedID}, TeamID: teamID, Subject: "Blocked", Status: store.TeamTaskStatusPending,
			OwnerAgentID: &workerID, WorkflowStepID: "blocked", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true,
		}})
		if _, err := teamStore.db.Exec(`UPDATE team_tasks SET status=$1,blocker_reason='missing key',escalation_status=$2,escalation_attempt_count=2,escalation_next_at=$3 WHERE id=$4`,
			store.TeamTaskStatusBlocked, store.TeamTaskEscalationPending, time.Now(), blockedID); err != nil {
			t.Fatal(err)
		}
		guard := pgStepActionGuard(store.WorkflowActionRetryBlocked, teamID, workflow.ID, blockedID, store.TeamWorkflowStatusRunning, store.TeamTaskStatusBlocked, "use staging key")
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

	t.Run("request revision", func(t *testing.T) {
		teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
		blockedID, activeID := uuid.New(), uuid.New()
		workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
			{BaseModel: store.BaseModel{ID: blockedID}, TeamID: teamID, Subject: "Blocked", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "blocked", WorkflowKind: store.TeamWorkflowTaskKindWork},
			{BaseModel: store.BaseModel{ID: activeID}, TeamID: teamID, Subject: "Active", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "active", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
		})
		token, future := uuid.New(), time.Now().Add(time.Hour)
		if _, err := teamStore.db.Exec(`UPDATE team_tasks SET status=$1,blocker_reason='needs revision',escalation_status=$2,escalation_attempt_count=3 WHERE id=$3`, store.TeamTaskStatusBlocked, store.TeamTaskEscalationPending, blockedID); err != nil {
			t.Fatal(err)
		}
		if _, err := teamStore.db.Exec(`UPDATE team_tasks SET status=$1,dispatch_token=$2,dispatch_lease_until=$3,locked_at=$3,lock_expires_at=$3 WHERE id=$4`, store.TeamTaskStatusInProgress, token, future, activeID); err != nil {
			t.Fatal(err)
		}
		guard := pgStepActionGuard(store.WorkflowActionRequestRevision, teamID, workflow.ID, blockedID, store.TeamWorkflowStatusRunning, store.TeamTaskStatusBlocked, "split the delivery step")
		result, err := teamStore.ApplyWorkflowAction(ctx, guard)
		if err != nil || result.Outcome != store.WorkflowActionApplied || result.Workflow.Status != store.TeamWorkflowStatusNeedsRevision {
			t.Fatalf("request revision result=%+v error=%v", result, err)
		}
		blocked, _ := teamStore.GetTask(ctx, blockedID)
		if blocked.Status != store.TeamTaskStatusBlocked || blocked.BlockerReason != "needs revision" || blocked.EscalationStatus != store.TeamTaskEscalationPending || blocked.EscalationAttemptCount != 3 {
			t.Fatalf("selected blocker changed: %+v", blocked)
		}
		active, _ := teamStore.GetTask(ctx, activeID)
		if active.Status != store.TeamTaskStatusPending || active.DispatchToken != nil || active.LockedAt != nil {
			t.Fatalf("active task was not paused: %+v", active)
		}
		replay, err := teamStore.ApplyWorkflowAction(ctx, guard)
		if err != nil || replay.Outcome != store.WorkflowActionAlreadyApplied {
			t.Fatalf("replay result=%+v error=%v", replay, err)
		}
	})

	t.Run("apply replan", func(t *testing.T) {
		teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
		doneID, blockedID := uuid.New(), uuid.New()
		workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
			{BaseModel: store.BaseModel{ID: doneID}, TeamID: teamID, Subject: "Done", Status: store.TeamTaskStatusCompleted, Result: pgRecoveryStrPtr("evidence"), OwnerAgentID: &workerID, WorkflowStepID: "gather", WorkflowKind: store.TeamWorkflowTaskKindWork},
			{BaseModel: store.BaseModel{ID: blockedID}, TeamID: teamID, Subject: "Blocked", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "deliver", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
		})
		// Only needs_revision + the exact revision + a selected current-revision
		// blocked step is a valid apply_replan target (matrix line 86).
		if _, err := teamStore.db.Exec(`UPDATE team_workflows SET status=$1 WHERE id=$2`, store.TeamWorkflowStatusNeedsRevision, workflow.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := teamStore.db.Exec(`UPDATE team_tasks SET status=$1,blocker_reason='needs new plan' WHERE id=$2`, store.TeamTaskStatusBlocked, blockedID); err != nil {
			t.Fatal(err)
		}
		newTaskID := uuid.New()
		newPlan := []byte(`{"schema_version":1,"goal":"replanned","steps":["revised"]}`)
		replan := store.WorkflowReplan{
			Guard: store.WorkflowActionGuard{
				Action: store.WorkflowActionApplyReplan, TeamID: teamID, WorkflowID: workflow.ID,
				ExpectedPlanRevision: 1, ExpectedStatus: store.TeamWorkflowStatusNeedsRevision,
				TaskID: &blockedID, ExpectedTaskStatus: store.TeamTaskStatusBlocked, Reason: "replan justification",
				Actor: store.WorkflowActionActor{Kind: store.WorkflowActorCoordinator, AgentID: &leadID},
			},
			CoordinatorID: leadID, CanonicalPlan: newPlan, PlanHash: fmt.Sprintf("%x", sha256.Sum256(newPlan)),
			Tasks: []store.TeamTaskData{
				{BaseModel: store.BaseModel{ID: newTaskID}, TeamID: teamID, Subject: "Revised terminal", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "deliver", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
			},
		}
		result, err := teamStore.CommitWorkflowReplan(ctx, replan)
		if err != nil || result.Outcome != store.WorkflowActionApplied || result.Workflow.Status != store.TeamWorkflowStatusRunning || result.Workflow.PlanRevision != 2 {
			t.Fatalf("apply replan result=%+v error=%v", result, err)
		}
		// Completed evidence is immutable; the selected blocked step goes stale.
		if done, _ := teamStore.GetTask(ctx, doneID); done.Status != store.TeamTaskStatusCompleted || done.Result == nil || *done.Result != "evidence" {
			t.Fatalf("completed evidence changed: %+v", done)
		}
		if old, _ := teamStore.GetTask(ctx, blockedID); old.Status != store.TeamTaskStatusStale {
			t.Fatalf("old blocked step = %v, want stale", old.Status)
		}
		if fresh, err := teamStore.GetTask(ctx, newTaskID); err != nil || fresh.PlanRevision != 2 || fresh.Status != store.TeamTaskStatusPending {
			t.Fatalf("new-revision task=%+v error=%v", fresh, err)
		}
		// Replay on the now-superseded revision is a Conflict, NOT AlreadyApplied:
		// the racing/duplicate replan lost the CAS and must not be treated as idempotent.
		replay, err := teamStore.CommitWorkflowReplan(ctx, replan)
		if err != nil || replay.Outcome != store.WorkflowActionConflict {
			t.Fatalf("apply replan replay=%+v error=%v, want Conflict", replay, err)
		}
	})

	t.Run("cancel workflow", func(t *testing.T) {
		teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
		doneID, terminalID := uuid.New(), uuid.New()
		workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{
			{BaseModel: store.BaseModel{ID: doneID}, TeamID: teamID, Subject: "Done", Status: store.TeamTaskStatusCompleted, Result: pgRecoveryStrPtr("kept"), OwnerAgentID: &workerID, WorkflowStepID: "done", WorkflowKind: store.TeamWorkflowTaskKindWork},
			{BaseModel: store.BaseModel{ID: terminalID}, TeamID: teamID, Subject: "Terminal", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID, WorkflowStepID: "terminal", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true},
		})
		guard := pgAdminActionGuard(store.WorkflowActionCancelWorkflow, teamID, workflow.ID, store.TeamWorkflowStatusRunning, 1, "operator cancelled")
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
		teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
		terminalID := uuid.New()
		workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{{
			BaseModel: store.BaseModel{ID: terminalID}, TeamID: teamID, Subject: "Done", Status: store.TeamTaskStatusCompleted,
			Result: pgRecoveryStrPtr("evidence"), OwnerAgentID: &workerID, WorkflowStepID: "terminal", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true,
		}})
		_, staleFinalizeToken, err := teamStore.ClaimWorkflowFinalization(ctx, workflow.ID, time.Now().Add(time.Minute))
		if err != nil {
			t.Fatalf("claim finalization: %v", err)
		}
		guard := pgAdminActionGuard(store.WorkflowActionFailWorkflow, teamID, workflow.ID, store.TeamWorkflowStatusRunning, 1, "cannot deliver safely")
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
		teamStore, ctx, leadID, _, teamID := pgRecoveryFixture(t)
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
		if _, err := teamStore.db.Exec(`UPDATE team_workflows SET expansion_attempt_count=2,last_expansion_error='timeout',expansion_token=$1,expansion_lease_until=$2,next_expansion_at=$3 WHERE id=$4`, uuid.New(), time.Now().Add(-time.Hour), time.Now().Add(time.Hour), workflow.ID); err != nil {
			t.Fatal(err)
		}
		guard := pgAdminActionGuard(store.WorkflowActionRetryExpansion, teamID, workflow.ID, store.TeamWorkflowStatusPendingExpansion, 1, "retry with restored provider")
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
		teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
		terminalID := uuid.New()
		workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{{
			BaseModel: store.BaseModel{ID: terminalID}, TeamID: teamID, Subject: "Done", Status: store.TeamTaskStatusCompleted,
			Result: pgRecoveryStrPtr("final"), OwnerAgentID: &workerID, WorkflowStepID: "terminal", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true,
		}})
		_, finalizeToken, err := teamStore.ClaimWorkflowFinalization(ctx, workflow.ID, time.Now().Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if err := teamStore.CompleteWorkflowFinalization(ctx, workflow.ID, finalizeToken, store.TeamWorkflowStatusCompleted, "final"); err != nil {
			t.Fatal(err)
		}
		if _, err := teamStore.db.Exec(`UPDATE team_workflows SET delivery_status=$1,delivery_attempt_count=$2,last_delivery_error='dead channel',next_delivery_at=NULL WHERE id=$3`, store.TeamWorkflowDeliveryDead, store.MaxWorkflowDeliveryAttempts, workflow.ID); err != nil {
			t.Fatal(err)
		}
		if _, _, err := teamStore.ClaimWorkflowDelivery(ctx, workflow.ID, time.Now().Add(time.Minute)); err == nil {
			t.Fatal("dead delivery was claimed without retry_delivery")
		}
		guard := pgAdminActionGuard(store.WorkflowActionRetryDelivery, teamID, workflow.ID, store.TeamWorkflowStatusCompleted, 1, "channel restored")
		result, err := teamStore.ApplyWorkflowAction(ctx, guard)
		if err != nil || result.Outcome != store.WorkflowActionApplied || result.Workflow.DeliveryStatus != store.TeamWorkflowDeliveryPending || result.Workflow.DeliveryAttemptCount != 0 || result.Workflow.LastDeliveryError != "dead channel" {
			t.Fatalf("retry delivery result=%+v error=%v", result, err)
		}
		if _, _, err := teamStore.ClaimWorkflowDelivery(ctx, workflow.ID, time.Now().Add(time.Minute)); err != nil {
			t.Fatalf("re-armed delivery was not claimable: %v", err)
		}
	})
}

func TestPGApplyWorkflowActionCoordinatorAttributionAndCommentRollback(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
	blockedID := uuid.New()
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{{
		BaseModel: store.BaseModel{ID: blockedID}, TeamID: teamID, Subject: "Blocked", Status: store.TeamTaskStatusPending,
		OwnerAgentID: &workerID, WorkflowStepID: "blocked", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true,
	}})
	if _, err := teamStore.db.Exec(`UPDATE team_tasks SET status=$1,blocker_reason='blocked' WHERE id=$2`, store.TeamTaskStatusBlocked, blockedID); err != nil {
		t.Fatal(err)
	}

	missingAgentID := uuid.New()
	guard := pgStepActionGuard(store.WorkflowActionRetryBlocked, teamID, workflow.ID, blockedID, store.TeamWorkflowStatusRunning, store.TeamTaskStatusBlocked, "retry as coordinator")
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

func TestPGApplyWorkflowActionGuardsAndConcurrency(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
	blockedID := uuid.New()
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{{
		BaseModel: store.BaseModel{ID: blockedID}, TeamID: teamID, Subject: "Blocked", Status: store.TeamTaskStatusPending,
		OwnerAgentID: &workerID, WorkflowStepID: "blocked", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true,
	}})
	if _, err := teamStore.db.Exec(`UPDATE team_tasks SET status=$1,blocker_reason='blocked' WHERE id=$2`, store.TeamTaskStatusBlocked, blockedID); err != nil {
		t.Fatal(err)
	}
	guard := pgStepActionGuard(store.WorkflowActionRetryBlocked, teamID, workflow.ID, blockedID, store.TeamWorkflowStatusRunning, store.TeamTaskStatusBlocked, "retry once")

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

func TestPGWorkflowActionReplayConcurrencyMatrix(t *testing.T) {
	const callers = 6
	for _, action := range store.AllWorkflowActions {
		t.Run(string(action), func(t *testing.T) {
			teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
			workflowID, commentTaskID, guard, commit := pgActionMatrixFixture(t, teamStore, ctx, leadID, workerID, teamID, action)

			// Tenant, team, status, revision, and (where relevant) task guards must
			// neither mutate nor emit an action comment.
			for _, bad := range pgActionMatrixBadGuards(ctx, guard, action) {
				result, err := pgApplyMatrixAction(bad.ctx, teamStore, bad.guard, commit)
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
					result, err := pgApplyMatrixAction(ctx, teamStore, guard, commit)
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

			replayResult, err := pgApplyMatrixAction(ctx, teamStore, guard, commit)
			if err != nil || replayResult.Outcome != pgActionMatrixReplay(action) {
				t.Fatalf("post-state %s result=%+v error=%v, want %v", action, replayResult, err, pgActionMatrixReplay(action))
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

type pgMatrixBadGuard struct {
	name  string
	ctx   context.Context
	guard store.WorkflowActionGuard
}

func pgActionMatrixBadGuards(ctx context.Context, guard store.WorkflowActionGuard, action store.WorkflowAction) []pgMatrixBadGuard {
	guards := []pgMatrixBadGuard{
		{"wrong tenant", store.WithTenantID(context.Background(), uuid.New()), guard},
		{"wrong team", ctx, guard}, {"wrong status", ctx, guard}, {"wrong revision", ctx, guard},
	}
	guards[1].guard.TeamID = uuid.New()
	guards[2].guard.ExpectedStatus = "wrong-status"
	guards[3].guard.ExpectedPlanRevision++
	if action.StepScoped() {
		bad := guard
		id := uuid.New()
		bad.TaskID = &id
		guards = append(guards, pgMatrixBadGuard{"wrong task", ctx, bad})
	}
	return guards
}

func pgApplyMatrixAction(ctx context.Context, teamStore *PGTeamStore, guard store.WorkflowActionGuard, commit *store.WorkflowReplan) (store.WorkflowActionResult, error) {
	if guard.Action == store.WorkflowActionApplyReplan {
		r := *commit
		r.Guard = guard
		return teamStore.CommitWorkflowReplan(ctx, r)
	}
	return teamStore.ApplyWorkflowAction(ctx, guard)
}

func pgActionMatrixReplay(action store.WorkflowAction) store.WorkflowActionOutcome {
	if action == store.WorkflowActionApplyReplan {
		return store.WorkflowActionConflict
	}
	return store.WorkflowActionAlreadyApplied
}

func pgActionMatrixFixture(t *testing.T, teamStore *PGTeamStore, ctx context.Context, leadID, workerID, teamID uuid.UUID, action store.WorkflowAction) (uuid.UUID, uuid.UUID, store.WorkflowActionGuard, *store.WorkflowReplan) {
	t.Helper()
	if action == store.WorkflowActionRetryExpansion {
		plan := []byte(`{"schema_version":1,"goal":"expand","steps":[]}`)
		workflow := &store.TeamWorkflowData{TeamID: teamID, Status: store.TeamWorkflowStatusPendingExpansion, CanonicalPlan: plan, SchemaVersion: 1, PlanHash: fmt.Sprintf("%x", sha256.Sum256(plan)), CoordinatorAgentID: leadID, CoordinatorAgentKey: "lead", OriginAgentID: leadID, OriginAgentKey: "lead", OriginRunID: "matrix-expand-" + uuid.NewString(), OriginSessionKey: "session", OriginChannel: "ws", OriginChatID: "user"}
		audit := &store.TeamTaskData{TeamID: teamID, Subject: "Audit", Status: store.TeamTaskStatusPending, OwnerAgentID: &leadID}
		if err := teamStore.CreatePendingWorkflowRequest(ctx, workflow, audit); err != nil {
			t.Fatal(err)
		}
		if _, err := teamStore.db.Exec(`UPDATE team_workflows SET expansion_token=$1,expansion_lease_until=$2,next_expansion_at=$3 WHERE id=$4`, uuid.New(), time.Now().Add(-time.Hour), time.Now().Add(time.Hour), workflow.ID); err != nil {
			t.Fatal(err)
		}
		return workflow.ID, audit.ID, pgAdminActionGuard(action, teamID, workflow.ID, workflow.Status, 1, "matrix retry expansion"), nil
	}
	terminal := action == store.WorkflowActionRetryDelivery
	taskID := uuid.New()
	taskStatus := store.TeamTaskStatusPending
	if terminal {
		taskStatus = store.TeamTaskStatusCompleted
	}
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{{BaseModel: store.BaseModel{ID: taskID}, TeamID: teamID, Subject: "Matrix", Status: taskStatus, OwnerAgentID: &workerID, WorkflowStepID: "step", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true}})
	if terminal {
		_, token, err := teamStore.ClaimWorkflowFinalization(ctx, workflow.ID, time.Now().Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if err := teamStore.CompleteWorkflowFinalization(ctx, workflow.ID, token, store.TeamWorkflowStatusCompleted, "done"); err != nil {
			t.Fatal(err)
		}
		if _, err := teamStore.db.Exec(`UPDATE team_workflows SET delivery_status=$1,next_delivery_at=NULL WHERE id=$2`, store.TeamWorkflowDeliveryDead, workflow.ID); err != nil {
			t.Fatal(err)
		}
		return workflow.ID, taskID, pgAdminActionGuard(action, teamID, workflow.ID, store.TeamWorkflowStatusCompleted, 1, "matrix retry delivery"), nil
	}
	if _, err := teamStore.db.Exec(`UPDATE team_tasks SET status=$1,blocker_reason='blocked' WHERE id=$2`, store.TeamTaskStatusBlocked, taskID); err != nil {
		t.Fatal(err)
	}
	switch action {
	case store.WorkflowActionRetryBlocked:
		return workflow.ID, taskID, pgStepActionGuard(action, teamID, workflow.ID, taskID, store.TeamWorkflowStatusRunning, store.TeamTaskStatusBlocked, "matrix retry"), nil
	case store.WorkflowActionRequestRevision:
		return workflow.ID, taskID, pgStepActionGuard(action, teamID, workflow.ID, taskID, store.TeamWorkflowStatusRunning, store.TeamTaskStatusBlocked, "matrix revision"), nil
	case store.WorkflowActionCancelWorkflow:
		return workflow.ID, taskID, pgAdminActionGuard(action, teamID, workflow.ID, store.TeamWorkflowStatusRunning, 1, "matrix cancel"), nil
	case store.WorkflowActionFailWorkflow:
		return workflow.ID, taskID, pgAdminActionGuard(action, teamID, workflow.ID, store.TeamWorkflowStatusRunning, 1, "matrix fail"), nil
	case store.WorkflowActionApplyReplan:
		if _, err := teamStore.db.Exec(`UPDATE team_workflows SET status=$1 WHERE id=$2`, store.TeamWorkflowStatusNeedsRevision, workflow.ID); err != nil {
			t.Fatal(err)
		}
		plan := []byte(`{"schema_version":1,"goal":"matrix replan","steps":["replacement"]}`)
		guard := pgStepActionGuard(action, teamID, workflow.ID, taskID, store.TeamWorkflowStatusNeedsRevision, store.TeamTaskStatusBlocked, "matrix replan")
		return workflow.ID, taskID, guard, &store.WorkflowReplan{Guard: guard, CoordinatorID: leadID, CanonicalPlan: plan, PlanHash: fmt.Sprintf("%x", sha256.Sum256(plan)), Tasks: []store.TeamTaskData{{BaseModel: store.BaseModel{ID: uuid.New()}, TeamID: teamID, Subject: "Replacement", Status: store.TeamTaskStatusPending, OwnerAgentID: &workerID, WorkflowStepID: "replacement", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true}}}
	}
	t.Fatalf("unhandled action %s", action)
	return uuid.Nil, uuid.Nil, store.WorkflowActionGuard{}, nil
}

func TestPGWorkflowCreationRejectsTaskScopeMismatch(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
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
			if err := teamStore.db.QueryRow(`SELECT COUNT(*) FROM team_workflows WHERE id=$1`, workflow.ID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("workflow persisted after rejected scope: count=%d error=%v", count, err)
			}
		})
	}
}

func TestPGClaimWorkflowDeliveryHonorsFutureRetry(t *testing.T) {
	teamStore, ctx, leadID, workerID, teamID := pgRecoveryFixture(t)
	workflow := pgMakeRunningWorkflow(t, teamStore, ctx, teamID, leadID, []store.TeamTaskData{{
		BaseModel: store.BaseModel{ID: uuid.New()}, TeamID: teamID, Subject: "Done", Status: store.TeamTaskStatusCompleted,
		Result: pgRecoveryStrPtr("final"), OwnerAgentID: &workerID, WorkflowStepID: "terminal", WorkflowKind: store.TeamWorkflowTaskKindWork, WorkflowTerminal: true,
	}})
	_, finalizeToken, err := teamStore.ClaimWorkflowFinalization(ctx, workflow.ID, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := teamStore.CompleteWorkflowFinalization(ctx, workflow.ID, finalizeToken, store.TeamWorkflowStatusCompleted, "final"); err != nil {
		t.Fatal(err)
	}
	if _, err := teamStore.db.Exec(`UPDATE team_workflows SET delivery_status=$1,next_delivery_at=$2 WHERE id=$3`, store.TeamWorkflowDeliveryPending, time.Now().Add(time.Hour), workflow.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := teamStore.ClaimWorkflowDelivery(ctx, workflow.ID, time.Now().Add(time.Minute)); err == nil {
		t.Fatal("future pending delivery was claimed early")
	}
}

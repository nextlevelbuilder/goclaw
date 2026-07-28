//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// This file mirrors internal/store/pg/team_workflows_recovery.go for the SQLite
// backend (Phase 4 store contract). A blocker on a workflow work task no longer
// mechanically fails the whole workflow the way the July-14 incident did: the
// coordinator resolves it with exactly one of these bounded, authorized
// transitions, and the automatic expansion/delivery retry loops are budget-
// capped so a transient failure can no longer retry forever. PG/SQLite parity is
// enforced at compile time by the var _ store.TeamWorkflowStore assertion in
// team_workflows_attempt.go.

// RetryBlockedWorkflowTask moves a blocked workflow task blocked→pending so the
// dispatcher re-issues it, bumps recovery_count, and clears the blocker and
// escalation state. The coordinator's revised instruction is carried into the
// next dispatch as a comment. The task keeps its owner and plan_revision; the
// next ClaimWorkflowTaskDispatch mints a fresh dispatch token, so any old
// (invalidated) attempt stays stale.
func (s *SQLiteTeamStore) RetryBlockedWorkflowTask(ctx context.Context, taskID, teamID uuid.UUID, instruction string) (store.WorkflowTaskTransition, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	var workflowID uuid.UUID
	var revision int
	res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,blocker_reason='',recovery_count=recovery_count+1,
		dispatch_token=NULL,dispatch_lease_until=NULL,locked_at=NULL,lock_expires_at=NULL,
		escalation_status=?,escalation_attempt_count=0,escalation_next_at=NULL,escalation_last_error='',updated_at=?
		WHERE id=? AND team_id=? AND tenant_id=? AND workflow_kind=? AND status=?`,
		store.TeamTaskStatusPending, store.TeamTaskEscalationDelivered, now,
		taskID, teamID, tid, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusBlocked)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return store.WorkflowTaskTransition{Outcome: store.WorkflowMutationStale}, nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT workflow_id,plan_revision FROM team_tasks WHERE id=? AND tenant_id=?`, taskID, tid).Scan(&workflowID, &revision); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	if instruction != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO team_task_comments (id, task_id, agent_id, user_id, content, comment_type, created_at, tenant_id)
			VALUES (?,?,NULL,'',?,?,?,?)`,
			store.GenNewID(), taskID, instruction, "recovery", now, tid); err != nil {
			return store.WorkflowTaskTransition{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	return store.WorkflowTaskTransition{
		Outcome:      store.WorkflowMutationApplied,
		WorkflowID:   workflowID,
		TaskStatus:   store.TeamTaskStatusPending,
		PlanRevision: revision,
	}, nil
}

// CommitWorkflowReplan atomically supersedes the current plan revision. The
// replacement plan MUST already be backend-validated and frozen by the caller
// (BuildPlanConstraint) — the store never accepts a raw model-supplied graph.
// It locks the workflow (via the tenant/team/revision predicate on the final
// UPDATE), verifies the optimistic ExpectedRevision, bumps plan_revision, stores
// the new canonical plan/hash, invalidates every active old-revision attempt
// (clearing tokens) and marks their non-terminal tasks stale (completed results
// stay immutable), inserts the new-revision tasks, and returns to running.
func (s *SQLiteTeamStore) CommitWorkflowReplan(ctx context.Context, replan store.WorkflowReplan) (store.WorkflowActionResult, error) {
	if err := replan.Guard.Validate(); err != nil {
		return store.WorkflowActionResult{}, err
	}
	if replan.Guard.Action != store.WorkflowActionApplyReplan {
		return store.WorkflowActionResult{}, store.ErrWorkflowActionInvalid
	}

	if err := store.ValidateWorkflowReplan(replan); err != nil {
		return store.WorkflowActionResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.WorkflowActionResult{}, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	var currentRevision int
	var status string
	var coordinatorID uuid.UUID
	if err := tx.QueryRowContext(ctx, `SELECT plan_revision,status,coordinator_agent_id FROM team_workflows WHERE id=? AND tenant_id=? AND team_id=?`,
		replan.Guard.WorkflowID, tid, replan.Guard.TeamID).Scan(&currentRevision, &status, &coordinatorID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.WorkflowActionResult{}, store.ErrTaskNotFound
		}
		return store.WorkflowActionResult{}, err
	}
	if replan.CoordinatorID != coordinatorID {
		return s.finishSQLiteAction(ctx, tx, tid, replan.Guard, store.WorkflowActionConflict)
	}
	if replan.Guard.ExpectedPlanRevision > 0 && replan.Guard.ExpectedPlanRevision != currentRevision {
		return s.finishSQLiteAction(ctx, tx, tid, replan.Guard, store.WorkflowActionConflict)
	}
	if status != store.TeamWorkflowStatusNeedsRevision {
		return s.finishSQLiteAction(ctx, tx, tid, replan.Guard, store.WorkflowActionConflict)
	}
	if replan.Guard.ExpectedStatus != "" && replan.Guard.ExpectedStatus != status {
		return s.finishSQLiteAction(ctx, tx, tid, replan.Guard, store.WorkflowActionConflict)
	}

	var taskStatus string
	var taskKind string
	var taskRev int
	err = tx.QueryRowContext(ctx, `SELECT status,COALESCE(workflow_kind,''),COALESCE(plan_revision,1) FROM team_tasks WHERE id=? AND team_id=? AND tenant_id=? AND workflow_id=?`,
		*replan.Guard.TaskID, replan.Guard.TeamID, tid, replan.Guard.WorkflowID).Scan(&taskStatus, &taskKind, &taskRev)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return s.finishSQLiteAction(ctx, tx, tid, replan.Guard, store.WorkflowActionConflict)
		}
		return store.WorkflowActionResult{}, err
	}
	if taskKind != store.TeamWorkflowTaskKindWork || taskRev != currentRevision || taskStatus != store.TeamTaskStatusBlocked {
		return s.finishSQLiteAction(ctx, tx, tid, replan.Guard, store.WorkflowActionConflict)
	}
	if replan.Guard.ExpectedTaskStatus != "" && replan.Guard.ExpectedTaskStatus != taskStatus {
		return s.finishSQLiteAction(ctx, tx, tid, replan.Guard, store.WorkflowActionConflict)
	}

	// Reserve the workflow row before mutating old or replacement tasks. SQLite
	// serializes writers at this conditional UPDATE; a stale contender returns a
	// Conflict without committing speculative task writes.
	res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET updated_at=?
		WHERE id=? AND tenant_id=? AND team_id=? AND status=? AND plan_revision=?`,
		now, replan.Guard.WorkflowID, tid, replan.Guard.TeamID,
		store.TeamWorkflowStatusNeedsRevision, currentRevision)
	if err != nil {
		return store.WorkflowActionResult{}, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return store.WorkflowActionResult{}, err
	}
	if n != 1 {
		return s.finishSQLiteAction(ctx, tx, tid, replan.Guard, store.WorkflowActionConflict)
	}

	newRevision := currentRevision + 1
	if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,dispatch_token=NULL,dispatch_lease_until=NULL,locked_at=NULL,lock_expires_at=NULL,
		blocker_reason='',escalation_status='delivered',escalation_attempt_count=0,escalation_next_at=NULL,escalation_last_error='',updated_at=?
		WHERE workflow_id=? AND tenant_id=? AND team_id=? AND workflow_kind=? AND plan_revision=? AND status IN (?,?,?,?,?)`,
		store.TeamTaskStatusStale, now, replan.Guard.WorkflowID, tid, replan.Guard.TeamID, store.TeamWorkflowTaskKindWork, currentRevision,
		store.TeamTaskStatusPending, store.TeamTaskStatusDispatching, store.TeamTaskStatusInProgress, store.TeamTaskStatusBlocked, store.TeamTaskStatusInReview); err != nil {
		return store.WorkflowActionResult{}, err
	}
	var terminalID *uuid.UUID
	for i := range replan.Tasks {
		replan.Tasks[i].TeamID = replan.Guard.TeamID
		replan.Tasks[i].WorkflowID = &replan.Guard.WorkflowID
		replan.Tasks[i].WorkflowKind = store.TeamWorkflowTaskKindWork
		replan.Tasks[i].PlanRevision = newRevision
		if err := insertSQLiteWorkflowTask(ctx, tx, &replan.Tasks[i]); err != nil {
			return store.WorkflowActionResult{}, err
		}
		if replan.Tasks[i].WorkflowTerminal {
			id := replan.Tasks[i].ID
			terminalID = &id
		}
	}
	res, err = tx.ExecContext(ctx, `UPDATE team_workflows SET status=?,plan_revision=?,canonical_plan=?,plan_hash=?,terminal_task_id=?,
		failure_summary='',failure_settle_deadline=NULL,updated_at=?
		WHERE id=? AND tenant_id=? AND team_id=? AND status=? AND plan_revision=?`,
		store.TeamWorkflowStatusRunning, newRevision, []byte(replan.CanonicalPlan), replan.PlanHash, terminalID, now,
		replan.Guard.WorkflowID, tid, replan.Guard.TeamID, store.TeamWorkflowStatusNeedsRevision, currentRevision)
	if err != nil {
		return store.WorkflowActionResult{}, err
	}
	n, err = res.RowsAffected()
	if err != nil {
		return store.WorkflowActionResult{}, err
	}
	if n != 1 {
		return store.WorkflowActionResult{}, fmt.Errorf("reserved workflow replan update affected %d rows", n)
	}
	if err := insertSQLiteActionComment(ctx, tx, tid, replan.Guard.TaskID, replan.Guard.Actor, replan.Guard.Reason, now); err != nil {
		return store.WorkflowActionResult{}, err
	}
	return s.finishSQLiteAction(ctx, tx, tid, replan.Guard, store.WorkflowActionApplied)
}

// CancelWorkflow performs an authorized workflow-level cancellation. It moves a
// non-terminal workflow to cancelling, records the reason, invalidates active
// attempts, and cancels every non-terminal work task (completed results are
// preserved). The finalizer then commits cancelling→cancelled with a durable
// summary.
func (s *SQLiteTeamStore) CancelWorkflow(ctx context.Context, workflowID, teamID uuid.UUID, reason string) (*store.TeamWorkflowData, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=?,cancel_reason=?,cancelled_at=?,updated_at=?
		WHERE id=? AND tenant_id=? AND team_id=? AND status IN (?,?,?)`,
		store.TeamWorkflowStatusCancelling, reason, now, now, workflowID, tid, teamID,
		store.TeamWorkflowStatusRunning, store.TeamWorkflowStatusNeedsRevision, store.TeamWorkflowStatusPendingExpansion)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, fmt.Errorf("workflow is not in a cancellable state")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,result=COALESCE(result,?),dispatch_token=NULL,dispatch_lease_until=NULL,locked_at=NULL,lock_expires_at=NULL,updated_at=?
		WHERE workflow_id=? AND tenant_id=? AND workflow_kind=? AND status NOT IN (?,?,?,?)`,
		store.TeamTaskStatusCancelled, "Cancelled: "+reason, now, workflowID, tid, store.TeamWorkflowTaskKindWork,
		store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled, store.TeamTaskStatusStale); err != nil {
		return nil, err
	}
	w, err := scanSQLiteWorkflow(tx.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE id=? AND tenant_id=?`, workflowID, tid))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return w, nil
}

// FailWorkflow performs an authorized coordinator-confirmed terminal failure. It
// moves a non-terminal workflow (running|needs_revision) → failing, records the
// user-facing reason as the failure summary, arms the settle deadline, and
// cancels every non-terminal work task (completed results are preserved). The
// finalizer then commits failing→failed with the durable summary.
func (s *SQLiteTeamStore) FailWorkflow(ctx context.Context, workflowID, teamID uuid.UUID, reason string) (*store.TeamWorkflowData, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	settleDeadline := now.Add(store.WorkflowFailureSettleDelay)
	res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=?,failure_summary=CASE WHEN failure_summary='' THEN ? ELSE failure_summary END,failure_settle_deadline=COALESCE(failure_settle_deadline,?),updated_at=?
		WHERE id=? AND tenant_id=? AND team_id=? AND status IN (?,?)`,
		store.TeamWorkflowStatusFailing, reason, settleDeadline, now, workflowID, tid, teamID,
		store.TeamWorkflowStatusRunning, store.TeamWorkflowStatusNeedsRevision)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, fmt.Errorf("workflow is not in a failable state")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,result=COALESCE(result,?),dispatch_token=NULL,dispatch_lease_until=NULL,locked_at=NULL,lock_expires_at=NULL,updated_at=?
		WHERE workflow_id=? AND tenant_id=? AND workflow_kind=? AND status NOT IN (?,?,?,?)`,
		store.TeamTaskStatusCancelled, "Cancelled because workflow failed", now, workflowID, tid, store.TeamWorkflowTaskKindWork,
		store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled, store.TeamTaskStatusStale); err != nil {
		return nil, err
	}
	w, err := scanSQLiteWorkflow(tx.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE id=? AND tenant_id=?`, workflowID, tid))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return w, nil
}

// FailWorkflowExpansion consumes one bounded expansion attempt. It only lands
// while the caller still holds the expansion token. transient=true schedules a
// capped-backoff retry (next_expansion_at, token cleared so the ticker can
// re-claim). Once the attempt budget is exhausted, or transient=false
// (deterministic plan/hash/roster invalidation), the workflow moves to failing
// so the finalizer emits a user-visible failure summary.
func (s *SQLiteTeamStore) FailWorkflowExpansion(ctx context.Context, workflowID, coordinatorID, expansionToken uuid.UUID, reason string, transient bool) (*store.TeamWorkflowData, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	var attempts int
	if err := tx.QueryRowContext(ctx, `SELECT expansion_attempt_count FROM team_workflows
		WHERE id=? AND tenant_id=? AND coordinator_agent_id=? AND expansion_token=? AND status=?`,
		workflowID, tid, coordinatorID, expansionToken, store.TeamWorkflowStatusPendingExpansion).Scan(&attempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("stale or unclaimed workflow expansion")
		}
		return nil, err
	}
	attempts++
	exhausted := !transient || attempts >= store.MaxWorkflowExpansionAttempts
	if exhausted {
		if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=?,expansion_attempt_count=?,last_expansion_error=?,expansion_token=NULL,expansion_lease_until=NULL,next_expansion_at=NULL,
			failure_summary=CASE WHEN failure_summary='' THEN ? ELSE failure_summary END,failure_settle_deadline=COALESCE(failure_settle_deadline,?),updated_at=?
			WHERE id=? AND tenant_id=?`,
			store.TeamWorkflowStatusFailing, attempts, reason, reason, now, now, workflowID, tid); err != nil {
			return nil, err
		}
	} else {
		nextAt := now.Add(store.WorkflowRetryBackoff(attempts))
		if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET expansion_attempt_count=?,last_expansion_error=?,expansion_token=NULL,expansion_lease_until=NULL,next_expansion_at=?,updated_at=?
			WHERE id=? AND tenant_id=?`,
			attempts, reason, nextAt, now, workflowID, tid); err != nil {
			return nil, err
		}
	}
	w, err := scanSQLiteWorkflow(tx.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE id=? AND tenant_id=?`, workflowID, tid))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return w, nil
}

// FailWorkflowDeliveryAttempt consumes one bounded delivery attempt. It only
// lands while the caller still holds the delivery token. It records the error and,
// while attempts remain, schedules a capped-backoff retry (next_delivery_at,
// delivery_status back to pending, token cleared). Once the budget is exhausted
// it marks delivery dead so an operator can see the last error while the result
// summary stays readable via the API/UI.
func (s *SQLiteTeamStore) FailWorkflowDeliveryAttempt(ctx context.Context, workflowID, deliveryToken uuid.UUID, reason string) (*store.TeamWorkflowData, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	var attempts int
	if err := tx.QueryRowContext(ctx, `SELECT delivery_attempt_count FROM team_workflows
		WHERE id=? AND tenant_id=? AND delivery_token=? AND delivered_at IS NULL`,
		workflowID, tid, deliveryToken).Scan(&attempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("stale workflow delivery token")
		}
		return nil, err
	}
	attempts++
	if attempts >= store.MaxWorkflowDeliveryAttempts {
		if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET delivery_status=?,delivery_attempt_count=?,last_delivery_error=?,delivery_token=NULL,delivery_lease_until=NULL,next_delivery_at=NULL,updated_at=?
			WHERE id=? AND tenant_id=?`,
			store.TeamWorkflowDeliveryDead, attempts, reason, now, workflowID, tid); err != nil {
			return nil, err
		}
	} else {
		nextAt := now.Add(store.WorkflowRetryBackoff(attempts))
		if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET delivery_status=?,delivery_attempt_count=?,last_delivery_error=?,delivery_token=NULL,delivery_lease_until=NULL,next_delivery_at=?,updated_at=?
			WHERE id=? AND tenant_id=?`,
			store.TeamWorkflowDeliveryPending, attempts, reason, nextAt, now, workflowID, tid); err != nil {
			return nil, err
		}
	}
	w, err := scanSQLiteWorkflow(tx.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE id=? AND tenant_id=?`, workflowID, tid))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return w, nil
}

// ListEscalationDueTasks returns blocked workflow work tasks whose coordinator
// escalation is due (escalation_status IN ('pending','enqueuing') AND
// escalation_next_at <= now). It is the cross-tenant sweep the recovery ticker
// runs to find blockers that still need a coordinator recovery run enqueued; the
// per-task ClaimTaskEscalation CAS then guards the actual enqueue.
func (s *SQLiteTeamStore) ListEscalationDueTasks(ctx context.Context, now time.Time) ([]store.TeamTaskData, error) {
	if !store.IsCrossTenant(ctx) {
		return nil, fmt.Errorf("cross-tenant workflow recovery required")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM team_tasks
		WHERE workflow_kind=? AND status=? AND escalation_status IN (?,?) AND escalation_next_at IS NOT NULL AND escalation_next_at<=?
		ORDER BY escalation_next_at LIMIT 100`,
		store.TeamWorkflowTaskKindWork, store.TeamTaskStatusBlocked,
		store.TeamTaskEscalationPending, store.TeamTaskEscalationEnqueuing, now)
	if err != nil {
		return nil, err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) == 0 {
		return nil, nil
	}
	return s.GetTasksByIDs(ctx, ids)
}

// ClaimTaskEscalation atomically claims one due escalation for enqueue. It locks
// the task row (via the tenant/team/status/due predicate on the guarded UPDATE),
// re-checks the due predicate (a concurrent coordinator resolution —
// retry/replan/cancel — moves the task out of blocked and drops it from the
// claim), bumps escalation_attempt_count, and while the budget remains moves the
// escalation pending|enqueuing → enqueuing and schedules the next capped-backoff
// re-claim (Claimed=true). Once MaxWorkflowEscalationAttempts is reached it
// instead marks the escalation dead and the workflow failing (Exhausted=true),
// so an unacknowledged blocker fails with a user-visible summary rather than
// being silently dropped the way the July-14 incident did.
func (s *SQLiteTeamStore) ClaimTaskEscalation(ctx context.Context, taskID, teamID uuid.UUID, now time.Time) (store.EscalationClaim, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.EscalationClaim{}, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	var attempts int
	var workflowID uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT escalation_attempt_count,workflow_id FROM team_tasks
		WHERE id=? AND team_id=? AND tenant_id=? AND workflow_kind=? AND status=? AND escalation_status IN (?,?) AND escalation_next_at IS NOT NULL AND escalation_next_at<=?`,
		taskID, teamID, tid, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusBlocked,
		store.TeamTaskEscalationPending, store.TeamTaskEscalationEnqueuing, now).Scan(&attempts, &workflowID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.EscalationClaim{}, nil
		}
		return store.EscalationClaim{}, err
	}
	attempts++
	claim := store.EscalationClaim{WorkflowID: workflowID, TeamID: teamID, TaskID: taskID, Attempt: attempts}
	if attempts >= store.MaxWorkflowEscalationAttempts {
		if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET escalation_status=?,escalation_attempt_count=?,escalation_next_at=NULL,updated_at=?
			WHERE id=? AND tenant_id=?`,
			store.TeamTaskEscalationDead, attempts, now, taskID, tid); err != nil {
			return store.EscalationClaim{}, err
		}
		reason := fmt.Sprintf("coordinator recovery unacknowledged after %d escalation attempts", attempts)
		if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=?,failure_summary=CASE WHEN failure_summary='' THEN ? ELSE failure_summary END,failure_settle_deadline=COALESCE(failure_settle_deadline,?),updated_at=?
			WHERE id=? AND tenant_id=? AND status IN (?,?)`,
			store.TeamWorkflowStatusFailing, reason, now, now, workflowID, tid,
			store.TeamWorkflowStatusRunning, store.TeamWorkflowStatusNeedsRevision); err != nil {
			return store.EscalationClaim{}, err
		}
		claim.Exhausted = true
	} else {
		nextAt := now.Add(store.WorkflowRetryBackoff(attempts))
		if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET escalation_status=?,escalation_attempt_count=?,escalation_next_at=?,updated_at=?
			WHERE id=? AND tenant_id=?`,
			store.TeamTaskEscalationEnqueuing, attempts, nextAt, now, taskID, tid); err != nil {
			return store.EscalationClaim{}, err
		}
		claim.Claimed = true
	}
	if err := tx.Commit(); err != nil {
		return store.EscalationClaim{}, err
	}
	return claim, nil
}

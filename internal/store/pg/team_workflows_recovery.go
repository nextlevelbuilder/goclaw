package pg

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// This file implements the coordinator-driven recovery transitions (Phase 4
// store contract). A blocker on a workflow work task no longer mechanically
// fails the whole workflow the way the July-14 incident did: the coordinator
// resolves it with exactly one of these bounded, authorized transitions, and
// the automatic expansion/delivery retry loops are budget-capped so a transient
// failure can no longer retry forever.

// RetryBlockedWorkflowTask moves a blocked workflow task blocked→pending so the
// dispatcher re-issues it, bumps recovery_count, and clears the blocker and
// escalation state. The coordinator's revised instruction is carried into the
// next dispatch as a comment. The task keeps its owner and plan_revision; the
// next ClaimWorkflowTaskDispatch mints a fresh dispatch token, so any old
// (invalidated) attempt stays stale.
func (s *PGTeamStore) RetryBlockedWorkflowTask(ctx context.Context, taskID, teamID uuid.UUID, instruction string) (store.WorkflowTaskTransition, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	var workflowID uuid.UUID
	var revision int
	res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=$1,blocker_reason='',recovery_count=recovery_count+1,
		dispatch_token=NULL,dispatch_lease_until=NULL,locked_at=NULL,lock_expires_at=NULL,
		escalation_status=$2,escalation_attempt_count=0,escalation_next_at=NULL,escalation_last_error='',updated_at=$3
		WHERE id=$4 AND team_id=$5 AND tenant_id=$6 AND workflow_kind=$7 AND status=$8`,
		store.TeamTaskStatusPending, store.TeamTaskEscalationDelivered, now,
		taskID, teamID, tid, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusBlocked)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return store.WorkflowTaskTransition{Outcome: store.WorkflowMutationStale}, nil
	}
	if err := tx.QueryRowContext(ctx, `SELECT workflow_id,plan_revision FROM team_tasks WHERE id=$1 AND tenant_id=$2`, taskID, tid).Scan(&workflowID, &revision); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	if instruction != "" {
		if _, err := tx.ExecContext(ctx, `INSERT INTO team_task_comments (id, task_id, agent_id, user_id, content, comment_type, created_at, tenant_id)
			VALUES ($1,$2,NULL,'',$3,$4,$5,$6)`,
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

// CancelWorkflow performs an authorized workflow-level cancellation. It moves a
// non-terminal workflow to cancelling, records the reason, invalidates active
// attempts, and cancels every non-terminal work task (completed results are
// preserved). The finalizer then commits cancelling→cancelled with a durable
// summary.
func (s *PGTeamStore) CancelWorkflow(ctx context.Context, workflowID, teamID uuid.UUID, reason string) (*store.TeamWorkflowData, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=$1,cancel_reason=$2,cancelled_at=$3,updated_at=$3
		WHERE id=$4 AND tenant_id=$5 AND team_id=$6 AND status IN ($7,$8,$9)`,
		store.TeamWorkflowStatusCancelling, reason, now, workflowID, tid, teamID,
		store.TeamWorkflowStatusRunning, store.TeamWorkflowStatusNeedsRevision, store.TeamWorkflowStatusPendingExpansion)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, fmt.Errorf("workflow is not in a cancellable state")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=$1,result=COALESCE(result,$2),dispatch_token=NULL,dispatch_lease_until=NULL,locked_at=NULL,lock_expires_at=NULL,updated_at=$3
		WHERE workflow_id=$4 AND tenant_id=$5 AND workflow_kind=$6 AND status NOT IN ($7,$8,$9,$10)`,
		store.TeamTaskStatusCancelled, "Cancelled: "+reason, now, workflowID, tid, store.TeamWorkflowTaskKindWork,
		store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled, store.TeamTaskStatusStale); err != nil {
		return nil, err
	}
	w, err := scanPGWorkflow(tx.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE id=$1 AND tenant_id=$2`, workflowID, tid))
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
func (s *PGTeamStore) FailWorkflow(ctx context.Context, workflowID, teamID uuid.UUID, reason string) (*store.TeamWorkflowData, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	settleDeadline := now.Add(store.WorkflowFailureSettleDelay)
	res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=$1,failure_summary=CASE WHEN failure_summary='' THEN $2 ELSE failure_summary END,failure_settle_deadline=COALESCE(failure_settle_deadline,$3),updated_at=$4
		WHERE id=$5 AND tenant_id=$6 AND team_id=$7 AND status IN ($8,$9)`,
		store.TeamWorkflowStatusFailing, reason, settleDeadline, now, workflowID, tid, teamID,
		store.TeamWorkflowStatusRunning, store.TeamWorkflowStatusNeedsRevision)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, fmt.Errorf("workflow is not in a failable state")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=$1,result=COALESCE(result,$2),dispatch_token=NULL,dispatch_lease_until=NULL,locked_at=NULL,lock_expires_at=NULL,updated_at=$3
		WHERE workflow_id=$4 AND tenant_id=$5 AND workflow_kind=$6 AND status NOT IN ($7,$8,$9,$10)`,
		store.TeamTaskStatusCancelled, "Cancelled because workflow failed", now, workflowID, tid, store.TeamWorkflowTaskKindWork,
		store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled, store.TeamTaskStatusStale); err != nil {
		return nil, err
	}
	w, err := scanPGWorkflow(tx.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE id=$1 AND tenant_id=$2`, workflowID, tid))
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
func (s *PGTeamStore) FailWorkflowExpansion(ctx context.Context, workflowID, coordinatorID, expansionToken uuid.UUID, reason string, transient bool) (*store.TeamWorkflowData, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	var attempts int
	if err := tx.QueryRowContext(ctx, `SELECT expansion_attempt_count FROM team_workflows
		WHERE id=$1 AND tenant_id=$2 AND coordinator_agent_id=$3 AND expansion_token=$4 AND status=$5 FOR UPDATE`,
		workflowID, tid, coordinatorID, expansionToken, store.TeamWorkflowStatusPendingExpansion).Scan(&attempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("stale or unclaimed workflow expansion")
		}
		return nil, err
	}
	attempts++
	exhausted := !transient || attempts >= store.MaxWorkflowExpansionAttempts
	if exhausted {
		if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=$1,expansion_attempt_count=$2,last_expansion_error=$3,expansion_token=NULL,expansion_lease_until=NULL,next_expansion_at=NULL,
			failure_summary=CASE WHEN failure_summary='' THEN $3 ELSE failure_summary END,failure_settle_deadline=COALESCE(failure_settle_deadline,$4),updated_at=$4
			WHERE id=$5 AND tenant_id=$6`,
			store.TeamWorkflowStatusFailing, attempts, reason, now, workflowID, tid); err != nil {
			return nil, err
		}
	} else {
		nextAt := now.Add(store.WorkflowRetryBackoff(attempts))
		if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET expansion_attempt_count=$1,last_expansion_error=$2,expansion_token=NULL,expansion_lease_until=NULL,next_expansion_at=$3,updated_at=$4
			WHERE id=$5 AND tenant_id=$6`,
			attempts, reason, nextAt, now, workflowID, tid); err != nil {
			return nil, err
		}
	}
	w, err := scanPGWorkflow(tx.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE id=$1 AND tenant_id=$2`, workflowID, tid))
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
func (s *PGTeamStore) FailWorkflowDeliveryAttempt(ctx context.Context, workflowID, deliveryToken uuid.UUID, reason string) (*store.TeamWorkflowData, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	var attempts int
	if err := tx.QueryRowContext(ctx, `SELECT delivery_attempt_count FROM team_workflows
		WHERE id=$1 AND tenant_id=$2 AND delivery_token=$3 AND delivered_at IS NULL FOR UPDATE`,
		workflowID, tid, deliveryToken).Scan(&attempts); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("stale workflow delivery token")
		}
		return nil, err
	}
	attempts++
	if attempts >= store.MaxWorkflowDeliveryAttempts {
		if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET delivery_status=$1,delivery_attempt_count=$2,last_delivery_error=$3,delivery_token=NULL,delivery_lease_until=NULL,next_delivery_at=NULL,updated_at=$4
			WHERE id=$5 AND tenant_id=$6`,
			store.TeamWorkflowDeliveryDead, attempts, reason, now, workflowID, tid); err != nil {
			return nil, err
		}
	} else {
		nextAt := now.Add(store.WorkflowRetryBackoff(attempts))
		if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET delivery_status=$1,delivery_attempt_count=$2,last_delivery_error=$3,delivery_token=NULL,delivery_lease_until=NULL,next_delivery_at=$4,updated_at=$5
			WHERE id=$6 AND tenant_id=$7`,
			store.TeamWorkflowDeliveryPending, attempts, reason, nextAt, now, workflowID, tid); err != nil {
			return nil, err
		}
	}
	w, err := scanPGWorkflow(tx.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE id=$1 AND tenant_id=$2`, workflowID, tid))
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
func (s *PGTeamStore) ListEscalationDueTasks(ctx context.Context, now time.Time) ([]store.TeamTaskData, error) {
	if !store.IsCrossTenant(ctx) {
		return nil, fmt.Errorf("cross-tenant workflow recovery required")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM team_tasks
		WHERE workflow_kind=$1 AND status=$2 AND escalation_status IN ($3,$4) AND escalation_next_at IS NOT NULL AND escalation_next_at<=$5
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
// the task row, re-checks the due predicate (a concurrent coordinator resolution
// — retry/replan/cancel — moves the task out of blocked and drops it from the
// claim), bumps escalation_attempt_count, and while the budget remains moves the
// escalation pending|enqueuing → enqueuing and schedules the next capped-backoff
// re-claim (Claimed=true). Once MaxWorkflowEscalationAttempts is reached it
// instead marks the escalation dead and the workflow failing (Exhausted=true),
// so an unacknowledged blocker fails with a user-visible summary rather than
// being silently dropped the way the July-14 incident did.
func (s *PGTeamStore) ClaimTaskEscalation(ctx context.Context, taskID, teamID uuid.UUID, now time.Time) (store.EscalationClaim, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.EscalationClaim{}, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	var attempts int
	var workflowID uuid.UUID
	err = tx.QueryRowContext(ctx, `SELECT escalation_attempt_count,workflow_id FROM team_tasks
		WHERE id=$1 AND team_id=$2 AND tenant_id=$3 AND workflow_kind=$4 AND status=$5 AND escalation_status IN ($6,$7) AND escalation_next_at IS NOT NULL AND escalation_next_at<=$8 FOR UPDATE`,
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
		if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET escalation_status=$1,escalation_attempt_count=$2,escalation_next_at=NULL,updated_at=$3
			WHERE id=$4 AND tenant_id=$5`,
			store.TeamTaskEscalationDead, attempts, now, taskID, tid); err != nil {
			return store.EscalationClaim{}, err
		}
		reason := fmt.Sprintf("coordinator recovery unacknowledged after %d escalation attempts", attempts)
		if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=$1,failure_summary=CASE WHEN failure_summary='' THEN $2 ELSE failure_summary END,failure_settle_deadline=COALESCE(failure_settle_deadline,$3),updated_at=$3
			WHERE id=$4 AND tenant_id=$5 AND status IN ($6,$7)`,
			store.TeamWorkflowStatusFailing, reason, now, workflowID, tid,
			store.TeamWorkflowStatusRunning, store.TeamWorkflowStatusNeedsRevision); err != nil {
			return store.EscalationClaim{}, err
		}
		claim.Exhausted = true
	} else {
		nextAt := now.Add(store.WorkflowRetryBackoff(attempts))
		if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET escalation_status=$1,escalation_attempt_count=$2,escalation_next_at=$3,updated_at=$4
			WHERE id=$5 AND tenant_id=$6`,
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

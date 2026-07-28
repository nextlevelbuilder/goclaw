package pg

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Compile-time parity guard: the TeamWorkflowStore interface is otherwise only
// checked at runtime (via .(store.TeamWorkflowStore) type assertions in
// consumers), so a backend that forgets a method fails silently as ok=false
// instead of at build time. This static assertion forces the mismatch to a
// compile error and keeps PG/SQLite in lockstep.
var _ store.TeamWorkflowStore = (*PGTeamStore)(nil)

// This file implements the attempt-fenced workflow task transitions. Every
// mutation is CAS-guarded on the full WorkflowTaskAttempt tuple
// (tenant/team/workflow/task/dispatch_token/plan_revision) plus the expected
// current status, so a superseded attempt (whose token was replaced by
// recovery, requeue, or replan) can never mutate the task, publish an event, or
// fail the workflow. A zero-row CAS is disambiguated into AlreadyApplied (the
// same attempt already reached the target state — idempotent replay) versus
// Stale (the token no longer matches — a newer attempt owns the task).

// probePGAttemptOutcome resolves a zero-row CAS into AlreadyApplied vs Stale by
// re-reading the task's current token/status. Called only when the CAS matched
// no rows, inside the same transaction so the read is consistent.
func probePGAttemptOutcome(ctx context.Context, q workflowRowScanner, attempt store.WorkflowTaskAttempt, targetStatuses ...string) store.WorkflowMutationOutcome {
	var status string
	var token *uuid.UUID
	var revision int
	if err := q.Scan(&status, &token, &revision); err != nil {
		// Row gone or unreadable: treat as stale (the attempt no longer owns it).
		return store.WorkflowMutationStale
	}
	if token != nil && *token == attempt.DispatchToken && revision == attempt.PlanRevision {
		for _, ts := range targetStatuses {
			if status == ts {
				return store.WorkflowMutationAlreadyApplied
			}
		}
	}
	return store.WorkflowMutationStale
}

// finalizeReadyForRevision reports whether the workflow has no non-terminal
// work task left in the current plan revision (so the finalizer can converge).
// Stale/failed/cancelled tasks from superseded revisions do not block.
func finalizeReadyForRevision(ctx context.Context, tx *sql.Tx, workflowID uuid.UUID, tid uuid.UUID, revision int) (bool, error) {
	var pending int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM team_tasks WHERE workflow_id=$1 AND tenant_id=$2 AND workflow_kind=$3
		 AND plan_revision=$4 AND status NOT IN ($5,$6,$7,$8)`,
		workflowID, tid, store.TeamWorkflowTaskKindWork, revision,
		store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled, store.TeamTaskStatusStale,
	).Scan(&pending); err != nil {
		return false, err
	}
	return pending == 0, nil
}

func (s *PGTeamStore) AcceptWorkflowTaskAttempt(ctx context.Context, attempt store.WorkflowTaskAttempt, lockExpiresAt time.Time) (store.WorkflowTaskTransition, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=$1,locked_at=$2,lock_expires_at=$3,dispatch_lease_until=NULL,updated_at=$2
		WHERE id=$4 AND team_id=$5 AND tenant_id=$6 AND workflow_id=$7 AND workflow_kind=$8 AND dispatch_token=$9 AND plan_revision=$10 AND status=$11`,
		store.TeamTaskStatusInProgress, now, lockExpiresAt, attempt.TaskID, attempt.TeamID, tid, attempt.WorkflowID,
		store.TeamWorkflowTaskKindWork, attempt.DispatchToken, attempt.PlanRevision, store.TeamTaskStatusDispatching)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	tr := store.WorkflowTaskTransition{WorkflowID: attempt.WorkflowID, PlanRevision: attempt.PlanRevision}
	if n, _ := res.RowsAffected(); n == 1 {
		tr.Outcome = store.WorkflowMutationApplied
		tr.TaskStatus = store.TeamTaskStatusInProgress
	} else {
		tr.Outcome = probePGAttemptOutcome(ctx,
			tx.QueryRowContext(ctx, `SELECT status,dispatch_token,plan_revision FROM team_tasks WHERE id=$1 AND tenant_id=$2`, attempt.TaskID, tid),
			attempt, store.TeamTaskStatusInProgress)
	}
	if err := tx.Commit(); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	return tr, nil
}

func (s *PGTeamStore) HeartbeatWorkflowTaskAttempt(ctx context.Context, attempt store.WorkflowTaskAttempt, lockExpiresAt time.Time) (store.WorkflowTaskTransition, error) {
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE team_tasks SET lock_expires_at=$1,updated_at=$2
		WHERE id=$3 AND team_id=$4 AND tenant_id=$5 AND workflow_id=$6 AND workflow_kind=$7 AND dispatch_token=$8 AND plan_revision=$9 AND status=$10`,
		lockExpiresAt, now, attempt.TaskID, attempt.TeamID, tid, attempt.WorkflowID,
		store.TeamWorkflowTaskKindWork, attempt.DispatchToken, attempt.PlanRevision, store.TeamTaskStatusInProgress)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	tr := store.WorkflowTaskTransition{WorkflowID: attempt.WorkflowID, PlanRevision: attempt.PlanRevision, TaskStatus: store.TeamTaskStatusInProgress}
	if n, _ := res.RowsAffected(); n == 1 {
		tr.Outcome = store.WorkflowMutationApplied
	} else {
		tr.Outcome = probePGAttemptOutcome(ctx,
			s.db.QueryRowContext(ctx, `SELECT status,dispatch_token,plan_revision FROM team_tasks WHERE id=$1 AND tenant_id=$2`, attempt.TaskID, tid),
			attempt, store.TeamTaskStatusInProgress)
	}
	return tr, nil
}

func (s *PGTeamStore) UpdateWorkflowTaskProgress(ctx context.Context, attempt store.WorkflowTaskAttempt, percent int, step string) (store.WorkflowTaskTransition, error) {
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE team_tasks SET progress_percent=$1,progress_step=$2,updated_at=$3
		WHERE id=$4 AND team_id=$5 AND tenant_id=$6 AND workflow_id=$7 AND workflow_kind=$8 AND dispatch_token=$9 AND plan_revision=$10 AND status=$11`,
		percent, step, now, attempt.TaskID, attempt.TeamID, tid, attempt.WorkflowID,
		store.TeamWorkflowTaskKindWork, attempt.DispatchToken, attempt.PlanRevision, store.TeamTaskStatusInProgress)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	tr := store.WorkflowTaskTransition{WorkflowID: attempt.WorkflowID, PlanRevision: attempt.PlanRevision, TaskStatus: store.TeamTaskStatusInProgress}
	if n, _ := res.RowsAffected(); n == 1 {
		tr.Outcome = store.WorkflowMutationApplied
	} else {
		tr.Outcome = probePGAttemptOutcome(ctx,
			s.db.QueryRowContext(ctx, `SELECT status,dispatch_token,plan_revision FROM team_tasks WHERE id=$1 AND tenant_id=$2`, attempt.TaskID, tid),
			attempt, store.TeamTaskStatusInProgress)
	}
	return tr, nil
}

func (s *PGTeamStore) CompleteWorkflowTaskAttempt(ctx context.Context, attempt store.WorkflowTaskAttempt, result string) (store.WorkflowTaskTransition, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=$1,result=$2,locked_at=NULL,lock_expires_at=NULL,dispatch_token=NULL,dispatch_lease_until=NULL,updated_at=$3
		WHERE id=$4 AND team_id=$5 AND tenant_id=$6 AND workflow_id=$7 AND workflow_kind=$8 AND dispatch_token=$9 AND plan_revision=$10 AND status=$11`,
		store.TeamTaskStatusCompleted, result, now, attempt.TaskID, attempt.TeamID, tid, attempt.WorkflowID,
		store.TeamWorkflowTaskKindWork, attempt.DispatchToken, attempt.PlanRevision, store.TeamTaskStatusInProgress)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	tr := store.WorkflowTaskTransition{WorkflowID: attempt.WorkflowID, PlanRevision: attempt.PlanRevision}
	if n, _ := res.RowsAffected(); n == 1 {
		tr.Outcome = store.WorkflowMutationApplied
		tr.TaskStatus = store.TeamTaskStatusCompleted
		if err := unblockDependentTasks(ctx, tx, attempt.TaskID); err != nil {
			return store.WorkflowTaskTransition{}, err
		}
		ready, err := finalizeReadyForRevision(ctx, tx, attempt.WorkflowID, tid, attempt.PlanRevision)
		if err != nil {
			return store.WorkflowTaskTransition{}, err
		}
		tr.ReadyToFinalize = ready
	} else {
		tr.Outcome = probePGAttemptOutcome(ctx,
			tx.QueryRowContext(ctx, `SELECT status,dispatch_token,plan_revision FROM team_tasks WHERE id=$1 AND tenant_id=$2`, attempt.TaskID, tid),
			attempt, store.TeamTaskStatusCompleted)
	}
	var wfStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM team_workflows WHERE id=$1 AND tenant_id=$2`, attempt.WorkflowID, tid).Scan(&wfStatus); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return store.WorkflowTaskTransition{}, err
	}
	tr.WorkflowStatus = wfStatus
	if err := tx.Commit(); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	return tr, nil
}

func (s *PGTeamStore) BlockWorkflowTaskAttempt(ctx context.Context, attempt store.WorkflowTaskAttempt, reason string) (store.WorkflowTaskTransition, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	// Blocking a task does NOT fail the workflow. It clears the attempt token
	// (invalidating the running attempt), records the blocker reason, and arms a
	// durable coordinator-escalation pending state so the recovery ticker can
	// retry enqueue if the immediate hand-off fails.
	res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=$1,blocker_reason=$2,locked_at=NULL,lock_expires_at=NULL,dispatch_token=NULL,dispatch_lease_until=NULL,
		escalation_status=$3,escalation_attempt_count=0,escalation_next_at=$4,escalation_last_error='',updated_at=$5
		WHERE id=$6 AND team_id=$7 AND tenant_id=$8 AND workflow_id=$9 AND workflow_kind=$10 AND dispatch_token=$11 AND plan_revision=$12 AND status=$13`,
		store.TeamTaskStatusBlocked, reason, store.TeamTaskEscalationPending, now, now,
		attempt.TaskID, attempt.TeamID, tid, attempt.WorkflowID, store.TeamWorkflowTaskKindWork,
		attempt.DispatchToken, attempt.PlanRevision, store.TeamTaskStatusInProgress)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	tr := store.WorkflowTaskTransition{WorkflowID: attempt.WorkflowID, PlanRevision: attempt.PlanRevision}
	if n, _ := res.RowsAffected(); n == 1 {
		tr.Outcome = store.WorkflowMutationApplied
		tr.TaskStatus = store.TeamTaskStatusBlocked
	} else {
		tr.Outcome = probePGAttemptOutcome(ctx,
			tx.QueryRowContext(ctx, `SELECT status,dispatch_token,plan_revision FROM team_tasks WHERE id=$1 AND tenant_id=$2`, attempt.TaskID, tid),
			attempt, store.TeamTaskStatusBlocked)
	}
	var wfStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM team_workflows WHERE id=$1 AND tenant_id=$2`, attempt.WorkflowID, tid).Scan(&wfStatus); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return store.WorkflowTaskTransition{}, err
	}
	tr.WorkflowStatus = wfStatus
	if err := tx.Commit(); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	return tr, nil
}

// RequeueWorkflowTaskAttempt returns a running attempt to pending after a
// TRANSIENT run failure, leaving the workflow `running`. It mirrors
// BlockWorkflowTaskAttempt's attempt-token guard (so a superseded attempt cannot
// resurrect a task) but arms no escalation: nobody needs to intervene, the step
// simply has to run again. dispatch_count is left untouched so maxTaskDispatches
// still bounds how many times this can happen.
func (s *PGTeamStore) RequeueWorkflowTaskAttempt(ctx context.Context, attempt store.WorkflowTaskAttempt, reason string) (store.WorkflowTaskTransition, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=$1,blocker_reason='',locked_at=NULL,lock_expires_at=NULL,dispatch_token=NULL,dispatch_lease_until=NULL,updated_at=$2
		WHERE id=$3 AND team_id=$4 AND tenant_id=$5 AND workflow_id=$6 AND workflow_kind=$7 AND dispatch_token=$8 AND plan_revision=$9 AND status=$10`,
		store.TeamTaskStatusPending, now,
		attempt.TaskID, attempt.TeamID, tid, attempt.WorkflowID, store.TeamWorkflowTaskKindWork,
		attempt.DispatchToken, attempt.PlanRevision, store.TeamTaskStatusInProgress)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	tr := store.WorkflowTaskTransition{WorkflowID: attempt.WorkflowID, PlanRevision: attempt.PlanRevision}
	if n, _ := res.RowsAffected(); n == 1 {
		tr.Outcome = store.WorkflowMutationApplied
		tr.TaskStatus = store.TeamTaskStatusPending
	} else {
		tr.Outcome = probePGAttemptOutcome(ctx,
			tx.QueryRowContext(ctx, `SELECT status,dispatch_token,plan_revision FROM team_tasks WHERE id=$1 AND tenant_id=$2`, attempt.TaskID, tid),
			attempt, store.TeamTaskStatusPending)
	}
	var wfStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM team_workflows WHERE id=$1 AND tenant_id=$2`, attempt.WorkflowID, tid).Scan(&wfStatus); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return store.WorkflowTaskTransition{}, err
	}
	tr.WorkflowStatus = wfStatus
	if err := tx.Commit(); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	return tr, nil
}

func (s *PGTeamStore) FailWorkflowTaskAttempt(ctx context.Context, attempt store.WorkflowTaskAttempt, reason string, failureSettleDeadline time.Time) (store.WorkflowTaskTransition, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=$1,result=$2,locked_at=NULL,lock_expires_at=NULL,dispatch_token=NULL,dispatch_lease_until=NULL,updated_at=$3
		WHERE id=$4 AND team_id=$5 AND tenant_id=$6 AND workflow_id=$7 AND workflow_kind=$8 AND dispatch_token=$9 AND plan_revision=$10 AND status=$11`,
		store.TeamTaskStatusFailed, "FAILED: "+reason, now, attempt.TaskID, attempt.TeamID, tid, attempt.WorkflowID,
		store.TeamWorkflowTaskKindWork, attempt.DispatchToken, attempt.PlanRevision, store.TeamTaskStatusInProgress)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	tr := store.WorkflowTaskTransition{WorkflowID: attempt.WorkflowID, PlanRevision: attempt.PlanRevision}
	if n, _ := res.RowsAffected(); n != 1 {
		tr.Outcome = probePGAttemptOutcome(ctx,
			tx.QueryRowContext(ctx, `SELECT status,dispatch_token,plan_revision FROM team_tasks WHERE id=$1 AND tenant_id=$2`, attempt.TaskID, tid),
			attempt, store.TeamTaskStatusFailed)
		var wfStatus string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM team_workflows WHERE id=$1 AND tenant_id=$2`, attempt.WorkflowID, tid).Scan(&wfStatus); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return store.WorkflowTaskTransition{}, err
		}
		tr.WorkflowStatus = wfStatus
		if err := tx.Commit(); err != nil {
			return store.WorkflowTaskTransition{}, err
		}
		return tr, nil
	}
	tr.Outcome = store.WorkflowMutationApplied
	tr.TaskStatus = store.TeamTaskStatusFailed
	if failureSettleDeadline.IsZero() {
		failureSettleDeadline = now.Add(2 * time.Minute)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=$1,failure_summary=CASE WHEN failure_summary='' THEN $2 ELSE failure_summary END,failure_settle_deadline=COALESCE(failure_settle_deadline,$3),updated_at=$4 WHERE id=$5 AND tenant_id=$6 AND status IN ($7,$1)`,
		store.TeamWorkflowStatusFailing, reason, failureSettleDeadline, now, attempt.WorkflowID, tid, store.TeamWorkflowStatusRunning); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	var wfStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM team_workflows WHERE id=$1 AND tenant_id=$2`, attempt.WorkflowID, tid).Scan(&wfStatus); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	tr.WorkflowStatus = wfStatus
	if err := tx.Commit(); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	return tr, nil
}

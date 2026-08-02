//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Compile-time parity guard (see the PG counterpart): the TeamWorkflowStore
// interface is consumed via runtime type assertions, so a missing method would
// otherwise fail silently as ok=false. This static assertion turns any
// PG/SQLite drift into a build error.
var _ store.TeamWorkflowStore = (*SQLiteTeamStore)(nil)

// This file mirrors internal/store/pg/team_workflows_attempt.go for the SQLite
// backend. Every workflow-task mutation is CAS-guarded on the full
// WorkflowTaskAttempt tuple (tenant/team/workflow/task/dispatch_token/plan_revision)
// plus the expected current status, so a superseded attempt can never mutate the
// task, publish an event, or fail the workflow. A zero-row CAS is disambiguated
// into AlreadyApplied (same attempt already at target — idempotent replay) versus
// Stale (token no longer matches — a newer attempt owns the task). PG and SQLite
// must keep identical transition/recovery semantics.

// probeSQLiteAttemptOutcome resolves a zero-row CAS into AlreadyApplied vs Stale
// by re-reading the task's current token/status/revision. Called only when the
// CAS matched no rows, inside the same transaction so the read is consistent.
//
// See probePGAttemptOutcome for the full rationale. The one subtlety it must
// preserve: a transition's own UPDATE clears dispatch_token to NULL on success,
// so when the post-turn settlement replays an attempt the tool path already
// settled, the token no longer matches the CAS. That replay must classify as
// AlreadyApplied (so settlement falls through to DispatchUnblockedTasks) rather
// than Stale (which would strand every dependent behind a completed root — the
// live G4 DAG defect). A newer plan_revision, or a non-nil token different from
// this attempt's, is a real supersession → Stale. PG and SQLite must keep
// identical transition/recovery semantics.
func probeSQLiteAttemptOutcome(ctx context.Context, q interface {
	Scan(...any) error
}, attempt store.WorkflowTaskAttempt, targetStatuses ...string) store.WorkflowMutationOutcome {
	var status string
	var token *uuid.UUID
	var revision int
	if err := q.Scan(&status, &token, &revision); err != nil {
		// Row gone or unreadable: treat as stale (the attempt no longer owns it).
		return store.WorkflowMutationStale
	}
	if revision != attempt.PlanRevision {
		return store.WorkflowMutationStale
	}
	if token != nil && *token != attempt.DispatchToken {
		return store.WorkflowMutationStale
	}
	for _, ts := range targetStatuses {
		if status == ts {
			return store.WorkflowMutationAlreadyApplied
		}
	}
	return store.WorkflowMutationStale
}

// finalizeReadyForRevisionSQLite reports whether the workflow has no non-terminal
// work task left in the current plan revision (so the finalizer can converge).
// Stale/failed/cancelled tasks from superseded revisions do not block.
func finalizeReadyForRevisionSQLite(ctx context.Context, tx *sql.Tx, workflowID, tid uuid.UUID, revision int) (bool, error) {
	var pending int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM team_tasks WHERE workflow_id=? AND tenant_id=? AND workflow_kind=?
		 AND plan_revision=? AND status NOT IN (?,?,?,?)`,
		workflowID, tid, store.TeamWorkflowTaskKindWork, revision,
		store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled, store.TeamTaskStatusStale,
	).Scan(&pending); err != nil {
		return false, err
	}
	return pending == 0, nil
}

func (s *SQLiteTeamStore) AcceptWorkflowTaskAttempt(ctx context.Context, attempt store.WorkflowTaskAttempt, lockExpiresAt time.Time) (store.WorkflowTaskTransition, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,locked_at=?,lock_expires_at=?,dispatch_lease_until=NULL,updated_at=?
		WHERE id=? AND team_id=? AND tenant_id=? AND workflow_id=? AND workflow_kind=? AND dispatch_token=? AND plan_revision=? AND status=?`,
		store.TeamTaskStatusInProgress, now, lockExpiresAt, now, attempt.TaskID, attempt.TeamID, tid, attempt.WorkflowID,
		store.TeamWorkflowTaskKindWork, attempt.DispatchToken, attempt.PlanRevision, store.TeamTaskStatusDispatching)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	tr := store.WorkflowTaskTransition{WorkflowID: attempt.WorkflowID, PlanRevision: attempt.PlanRevision}
	if n, _ := res.RowsAffected(); n == 1 {
		tr.Outcome = store.WorkflowMutationApplied
		tr.TaskStatus = store.TeamTaskStatusInProgress
	} else {
		tr.Outcome = probeSQLiteAttemptOutcome(ctx,
			tx.QueryRowContext(ctx, `SELECT status,dispatch_token,plan_revision FROM team_tasks WHERE id=? AND tenant_id=?`, attempt.TaskID, tid),
			attempt, store.TeamTaskStatusInProgress)
	}
	if err := tx.Commit(); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	return tr, nil
}

func (s *SQLiteTeamStore) HeartbeatWorkflowTaskAttempt(ctx context.Context, attempt store.WorkflowTaskAttempt, lockExpiresAt time.Time) (store.WorkflowTaskTransition, error) {
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE team_tasks SET lock_expires_at=?,updated_at=?
		WHERE id=? AND team_id=? AND tenant_id=? AND workflow_id=? AND workflow_kind=? AND dispatch_token=? AND plan_revision=? AND status=?`,
		lockExpiresAt, now, attempt.TaskID, attempt.TeamID, tid, attempt.WorkflowID,
		store.TeamWorkflowTaskKindWork, attempt.DispatchToken, attempt.PlanRevision, store.TeamTaskStatusInProgress)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	tr := store.WorkflowTaskTransition{WorkflowID: attempt.WorkflowID, PlanRevision: attempt.PlanRevision, TaskStatus: store.TeamTaskStatusInProgress}
	if n, _ := res.RowsAffected(); n == 1 {
		tr.Outcome = store.WorkflowMutationApplied
	} else {
		tr.Outcome = probeSQLiteAttemptOutcome(ctx,
			s.db.QueryRowContext(ctx, `SELECT status,dispatch_token,plan_revision FROM team_tasks WHERE id=? AND tenant_id=?`, attempt.TaskID, tid),
			attempt, store.TeamTaskStatusInProgress)
	}
	return tr, nil
}

func (s *SQLiteTeamStore) UpdateWorkflowTaskProgress(ctx context.Context, attempt store.WorkflowTaskAttempt, percent int, step string) (store.WorkflowTaskTransition, error) {
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE team_tasks SET progress_percent=?,progress_step=?,updated_at=?
		WHERE id=? AND team_id=? AND tenant_id=? AND workflow_id=? AND workflow_kind=? AND dispatch_token=? AND plan_revision=? AND status=?`,
		percent, step, now, attempt.TaskID, attempt.TeamID, tid, attempt.WorkflowID,
		store.TeamWorkflowTaskKindWork, attempt.DispatchToken, attempt.PlanRevision, store.TeamTaskStatusInProgress)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	tr := store.WorkflowTaskTransition{WorkflowID: attempt.WorkflowID, PlanRevision: attempt.PlanRevision, TaskStatus: store.TeamTaskStatusInProgress}
	if n, _ := res.RowsAffected(); n == 1 {
		tr.Outcome = store.WorkflowMutationApplied
	} else {
		tr.Outcome = probeSQLiteAttemptOutcome(ctx,
			s.db.QueryRowContext(ctx, `SELECT status,dispatch_token,plan_revision FROM team_tasks WHERE id=? AND tenant_id=?`, attempt.TaskID, tid),
			attempt, store.TeamTaskStatusInProgress)
	}
	return tr, nil
}

func (s *SQLiteTeamStore) CompleteWorkflowTaskAttempt(ctx context.Context, attempt store.WorkflowTaskAttempt, result string) (store.WorkflowTaskTransition, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,result=?,locked_at=NULL,lock_expires_at=NULL,dispatch_token=NULL,dispatch_lease_until=NULL,updated_at=?
		WHERE id=? AND team_id=? AND tenant_id=? AND workflow_id=? AND workflow_kind=? AND dispatch_token=? AND plan_revision=? AND status=?`,
		store.TeamTaskStatusCompleted, result, now, attempt.TaskID, attempt.TeamID, tid, attempt.WorkflowID,
		store.TeamWorkflowTaskKindWork, attempt.DispatchToken, attempt.PlanRevision, store.TeamTaskStatusInProgress)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	tr := store.WorkflowTaskTransition{WorkflowID: attempt.WorkflowID, PlanRevision: attempt.PlanRevision}
	if n, _ := res.RowsAffected(); n == 1 {
		tr.Outcome = store.WorkflowMutationApplied
		tr.TaskStatus = store.TeamTaskStatusCompleted
		if err := unblockDependentTasksSQLite(ctx, tx, attempt.TaskID); err != nil {
			return store.WorkflowTaskTransition{}, err
		}
		ready, err := finalizeReadyForRevisionSQLite(ctx, tx, attempt.WorkflowID, tid, attempt.PlanRevision)
		if err != nil {
			return store.WorkflowTaskTransition{}, err
		}
		tr.ReadyToFinalize = ready
	} else {
		tr.Outcome = probeSQLiteAttemptOutcome(ctx,
			tx.QueryRowContext(ctx, `SELECT status,dispatch_token,plan_revision FROM team_tasks WHERE id=? AND tenant_id=?`, attempt.TaskID, tid),
			attempt, store.TeamTaskStatusCompleted)
		// Mirror PG: compute ReadyToFinalize on the idempotent replay path, or
		// settle skips finalizeWorkflow and strands the workflow running with all
		// tasks completed. Finalization is claim-guarded + idempotent.
		if tr.Outcome == store.WorkflowMutationAlreadyApplied {
			ready, err := finalizeReadyForRevisionSQLite(ctx, tx, attempt.WorkflowID, tid, attempt.PlanRevision)
			if err != nil {
				return store.WorkflowTaskTransition{}, err
			}
			tr.ReadyToFinalize = ready
		}
	}
	var wfStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM team_workflows WHERE id=? AND tenant_id=?`, attempt.WorkflowID, tid).Scan(&wfStatus); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return store.WorkflowTaskTransition{}, err
	}
	tr.WorkflowStatus = wfStatus
	if err := tx.Commit(); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	return tr, nil
}

func (s *SQLiteTeamStore) BlockWorkflowTaskAttempt(ctx context.Context, attempt store.WorkflowTaskAttempt, reason string) (store.WorkflowTaskTransition, error) {
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
	res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,blocker_reason=?,locked_at=NULL,lock_expires_at=NULL,dispatch_token=NULL,dispatch_lease_until=NULL,
		escalation_status=?,escalation_attempt_count=0,escalation_next_at=?,escalation_last_error='',updated_at=?
		WHERE id=? AND team_id=? AND tenant_id=? AND workflow_id=? AND workflow_kind=? AND dispatch_token=? AND plan_revision=? AND status=?`,
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
		tr.Outcome = probeSQLiteAttemptOutcome(ctx,
			tx.QueryRowContext(ctx, `SELECT status,dispatch_token,plan_revision FROM team_tasks WHERE id=? AND tenant_id=?`, attempt.TaskID, tid),
			attempt, store.TeamTaskStatusBlocked)
	}
	var wfStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM team_workflows WHERE id=? AND tenant_id=?`, attempt.WorkflowID, tid).Scan(&wfStatus); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return store.WorkflowTaskTransition{}, err
	}
	tr.WorkflowStatus = wfStatus
	if err := tx.Commit(); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	return tr, nil
}

// RequeueWorkflowTaskAttempt returns a running attempt to pending after a
// TRANSIENT run failure, leaving the workflow `running`. See the PG twin and the
// store-interface doc: dispatch_count is deliberately preserved so
// maxTaskDispatches still bounds retries.
func (s *SQLiteTeamStore) RequeueWorkflowTaskAttempt(ctx context.Context, attempt store.WorkflowTaskAttempt, reason string) (store.WorkflowTaskTransition, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,blocker_reason='',locked_at=NULL,lock_expires_at=NULL,dispatch_token=NULL,dispatch_lease_until=NULL,updated_at=?
		WHERE id=? AND team_id=? AND tenant_id=? AND workflow_id=? AND workflow_kind=? AND dispatch_token=? AND plan_revision=? AND status=?`,
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
		tr.Outcome = probeSQLiteAttemptOutcome(ctx,
			tx.QueryRowContext(ctx, `SELECT status,dispatch_token,plan_revision FROM team_tasks WHERE id=? AND tenant_id=?`, attempt.TaskID, tid),
			attempt, store.TeamTaskStatusPending)
	}
	var wfStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM team_workflows WHERE id=? AND tenant_id=?`, attempt.WorkflowID, tid).Scan(&wfStatus); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return store.WorkflowTaskTransition{}, err
	}
	tr.WorkflowStatus = wfStatus
	if err := tx.Commit(); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	return tr, nil
}

func (s *SQLiteTeamStore) FailWorkflowTaskAttempt(ctx context.Context, attempt store.WorkflowTaskAttempt, reason string, failureSettleDeadline time.Time) (store.WorkflowTaskTransition, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,result=?,locked_at=NULL,lock_expires_at=NULL,dispatch_token=NULL,dispatch_lease_until=NULL,updated_at=?
		WHERE id=? AND team_id=? AND tenant_id=? AND workflow_id=? AND workflow_kind=? AND dispatch_token=? AND plan_revision=? AND status=?`,
		store.TeamTaskStatusFailed, "FAILED: "+reason, now, attempt.TaskID, attempt.TeamID, tid, attempt.WorkflowID,
		store.TeamWorkflowTaskKindWork, attempt.DispatchToken, attempt.PlanRevision, store.TeamTaskStatusInProgress)
	if err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	tr := store.WorkflowTaskTransition{WorkflowID: attempt.WorkflowID, PlanRevision: attempt.PlanRevision}
	if n, _ := res.RowsAffected(); n != 1 {
		tr.Outcome = probeSQLiteAttemptOutcome(ctx,
			tx.QueryRowContext(ctx, `SELECT status,dispatch_token,plan_revision FROM team_tasks WHERE id=? AND tenant_id=?`, attempt.TaskID, tid),
			attempt, store.TeamTaskStatusFailed)
		var wfStatus string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM team_workflows WHERE id=? AND tenant_id=?`, attempt.WorkflowID, tid).Scan(&wfStatus); err != nil && !errors.Is(err, sql.ErrNoRows) {
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
	if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=?,failure_summary=CASE WHEN failure_summary='' THEN ? ELSE failure_summary END,failure_settle_deadline=COALESCE(failure_settle_deadline,?),updated_at=? WHERE id=? AND tenant_id=? AND status IN (?,?)`,
		store.TeamWorkflowStatusFailing, reason, failureSettleDeadline, now, attempt.WorkflowID, tid, store.TeamWorkflowStatusRunning, store.TeamWorkflowStatusFailing); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	var wfStatus string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM team_workflows WHERE id=? AND tenant_id=?`, attempt.WorkflowID, tid).Scan(&wfStatus); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	tr.WorkflowStatus = wfStatus
	if err := tx.Commit(); err != nil {
		return store.WorkflowTaskTransition{}, err
	}
	return tr, nil
}

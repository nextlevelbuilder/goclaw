//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// This file is the SQLite twin of internal/store/pg/team_workflows_actions.go.
// It implements ApplyWorkflowAction — the single authoritative store transition
// for the five operator/coordinator recovery actions (retry_blocked,
// cancel_workflow, fail_workflow, retry_expansion, retry_delivery). Each branch
// opens one transaction, reads the authoritative workflow row (and the target
// task for step-scoped actions), enforces the optimistic
// ExpectedStatus/ExpectedPlanRevision/ExpectedTaskStatus guards, decides Applied
// vs AlreadyApplied vs Conflict, mutates at most once via status-guarded
// conditional UPDATEs, writes the actor-attributed comment only on Applied, then
// reloads the authoritative workflow + tasks before commit. The predicate and
// outcome logic is identical to the PG implementation.

// ApplyWorkflowAction is the SQLite implementation of the shared action contract.
func (s *SQLiteTeamStore) ApplyWorkflowAction(ctx context.Context, guard store.WorkflowActionGuard) (store.WorkflowActionResult, error) {
	if err := guard.Validate(); err != nil {
		return store.WorkflowActionResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return store.WorkflowActionResult{}, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	reason := strings.TrimSpace(guard.Reason)

	// Read the authoritative workflow row. Tenant + team scope come from
	// context/guard; a caller can never act across tenants by forging a field.
	var (
		status              string
		revision            int
		deliveryStatus      string
		deliveredAtValue    nullSqliteTime
		deliveryToken       *uuid.UUID
		deliveryLeaseValue  nullSqliteTime
		expansionToken      *uuid.UUID
		expansionLeaseValue nullSqliteTime
		nextExpansionValue  nullSqliteTime
		auditTaskID         *uuid.UUID
		terminalTaskID      *uuid.UUID
		nextDeliveryValue   nullSqliteTime
	)
	err = tx.QueryRowContext(ctx, `SELECT status,plan_revision,delivery_status,delivered_at,delivery_token,delivery_lease_until,
		expansion_token,expansion_lease_until,next_expansion_at,audit_task_id,terminal_task_id,next_delivery_at
		FROM team_workflows WHERE id=? AND tenant_id=? AND team_id=?`,
		guard.WorkflowID, tid, guard.TeamID).Scan(&status, &revision, &deliveryStatus, &deliveredAtValue, &deliveryToken, &deliveryLeaseValue,
		&expansionToken, &expansionLeaseValue, &nextExpansionValue, &auditTaskID, &terminalTaskID, &nextDeliveryValue)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.WorkflowActionResult{}, store.ErrTaskNotFound
		}
		return store.WorkflowActionResult{}, err
	}

	// Global optimistic revision guard: a stale caller (or a revision that moved
	// under a concurrent replan) reconciles against a fresh fetch.
	if guard.ExpectedPlanRevision != revision {
		return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionConflict)
	}
	statusOK := guard.ExpectedStatus == status

	// Step-scoped actions read the target task too.
	var (
		taskStatus string
		taskKind   string
		taskRev    int
		taskFound  bool
	)
	if guard.Action.StepScoped() {
		terr := tx.QueryRowContext(ctx, `SELECT status,COALESCE(workflow_kind,''),COALESCE(plan_revision,1)
			FROM team_tasks WHERE id=? AND team_id=? AND tenant_id=? AND workflow_id=?`,
			*guard.TaskID, guard.TeamID, tid, guard.WorkflowID).Scan(&taskStatus, &taskKind, &taskRev)
		switch {
		case errors.Is(terr, sql.ErrNoRows):
			taskFound = false
		case terr != nil:
			return store.WorkflowActionResult{}, terr
		default:
			taskFound = true
		}
	}
	taskStatusOK := !guard.Action.StepScoped() || guard.ExpectedTaskStatus == taskStatus

	switch guard.Action {
	case store.WorkflowActionRetryBlocked:
		canApply := taskFound && taskKind == store.TeamWorkflowTaskKindWork && taskRev == revision &&
			taskStatus == store.TeamTaskStatusBlocked &&
			(status == store.TeamWorkflowStatusRunning || status == store.TeamWorkflowStatusNeedsRevision)
		if canApply && statusOK && taskStatusOK {
			res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,blocker_reason='',recovery_count=recovery_count+1,
				dispatch_token=NULL,dispatch_lease_until=NULL,locked_at=NULL,lock_expires_at=NULL,
				escalation_status=?,escalation_attempt_count=0,escalation_next_at=NULL,escalation_last_error='',updated_at=?
				WHERE id=? AND team_id=? AND tenant_id=? AND workflow_id=? AND workflow_kind=? AND plan_revision=? AND status=?`,
				store.TeamTaskStatusPending, store.TeamTaskEscalationDelivered, now,
				*guard.TaskID, guard.TeamID, tid, guard.WorkflowID, store.TeamWorkflowTaskKindWork, revision, store.TeamTaskStatusBlocked)
			if err != nil {
				return store.WorkflowActionResult{}, err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return store.WorkflowActionResult{}, err
			}
			if n != 1 {
				var (
					curStatus string
					curRev    int
				)
				err := tx.QueryRowContext(ctx, `SELECT status,COALESCE(plan_revision,1) FROM team_tasks WHERE id=? AND team_id=? AND tenant_id=? AND workflow_id=?`,
					*guard.TaskID, guard.TeamID, tid, guard.WorkflowID).Scan(&curStatus, &curRev)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return store.WorkflowActionResult{}, err
				}
				if err == nil && curRev == revision && curStatus == store.TeamTaskStatusPending {
					return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionAlreadyApplied)
				}
				return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionConflict)
			}
			// needs_revision → running so the paused old-plan pending tasks resume.
			if status == store.TeamWorkflowStatusNeedsRevision {
				res, err = tx.ExecContext(ctx, `UPDATE team_workflows SET status=?,updated_at=? WHERE id=? AND tenant_id=? AND team_id=? AND status=? AND plan_revision=?`,
					store.TeamWorkflowStatusRunning, now, guard.WorkflowID, tid, guard.TeamID, store.TeamWorkflowStatusNeedsRevision, revision)
				if err != nil {
					return store.WorkflowActionResult{}, err
				}
				if err := requireOneSQLiteWorkflowActionRow(res); err != nil {
					return store.WorkflowActionResult{}, err
				}
			}
			if err := insertSQLiteActionComment(ctx, tx, tid, guard.TaskID, guard.Actor, reason, now); err != nil {
				return store.WorkflowActionResult{}, err
			}
			return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionApplied)
		}
		// Idempotent replay: the blocked task is already pending at the same
		// revision and the workflow is running again.
		if taskFound && taskRev == revision && taskStatus == store.TeamTaskStatusPending && status == store.TeamWorkflowStatusRunning {
			return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionAlreadyApplied)
		}
		return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionConflict)

	case store.WorkflowActionCancelWorkflow:
		canApply := status == store.TeamWorkflowStatusPendingExpansion || status == store.TeamWorkflowStatusRunning || status == store.TeamWorkflowStatusNeedsRevision
		if canApply && statusOK {
			res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=?,cancel_reason=?,cancelled_at=?,
				expansion_token=NULL,expansion_lease_until=NULL,finalize_token=NULL,finalize_lease_until=NULL,updated_at=?
				WHERE id=? AND tenant_id=? AND team_id=? AND status=? AND plan_revision=?`,
				store.TeamWorkflowStatusCancelling, reason, now, now, guard.WorkflowID, tid, guard.TeamID, status, revision)
			if err != nil {
				return store.WorkflowActionResult{}, err
			}
			if err := requireOneSQLiteWorkflowActionRow(res); err != nil {
				return store.WorkflowActionResult{}, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,result=COALESCE(result,?),dispatch_token=NULL,dispatch_lease_until=NULL,locked_at=NULL,lock_expires_at=NULL,updated_at=?
				WHERE workflow_id=? AND tenant_id=? AND team_id=? AND workflow_kind=? AND status NOT IN (?,?,?,?)`,
				store.TeamTaskStatusCancelled, "Cancelled: "+reason, now, guard.WorkflowID, tid, guard.TeamID, store.TeamWorkflowTaskKindWork,
				store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled, store.TeamTaskStatusStale); err != nil {
				return store.WorkflowActionResult{}, err
			}
			if err := insertSQLiteActionComment(ctx, tx, tid, cancelCommentTarget(auditTaskID, terminalTaskID), guard.Actor, reason, now); err != nil {
				return store.WorkflowActionResult{}, err
			}
			return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionApplied)
		}
		if status == store.TeamWorkflowStatusCancelling || status == store.TeamWorkflowStatusCancelled {
			return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionAlreadyApplied)
		}
		return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionConflict)

	case store.WorkflowActionFailWorkflow:
		canApply := status == store.TeamWorkflowStatusRunning || status == store.TeamWorkflowStatusNeedsRevision
		if canApply && statusOK {
			settleDeadline := now.Add(store.WorkflowFailureSettleDelay)
			res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=?,failure_summary=CASE WHEN failure_summary='' THEN ? ELSE failure_summary END,
				failure_settle_deadline=COALESCE(failure_settle_deadline,?),finalize_token=NULL,finalize_lease_until=NULL,updated_at=?
				WHERE id=? AND tenant_id=? AND team_id=? AND status=? AND plan_revision=?`,
				store.TeamWorkflowStatusFailing, reason, settleDeadline, now, guard.WorkflowID, tid, guard.TeamID, status, revision)
			if err != nil {
				return store.WorkflowActionResult{}, err
			}
			if err := requireOneSQLiteWorkflowActionRow(res); err != nil {
				return store.WorkflowActionResult{}, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,result=COALESCE(result,?),dispatch_token=NULL,dispatch_lease_until=NULL,locked_at=NULL,lock_expires_at=NULL,updated_at=?
				WHERE workflow_id=? AND tenant_id=? AND team_id=? AND workflow_kind=? AND status NOT IN (?,?,?,?)`,
				store.TeamTaskStatusCancelled, "Cancelled because workflow failed", now, guard.WorkflowID, tid, guard.TeamID, store.TeamWorkflowTaskKindWork,
				store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled, store.TeamTaskStatusStale); err != nil {
				return store.WorkflowActionResult{}, err
			}
			if err := insertSQLiteActionComment(ctx, tx, tid, cancelCommentTarget(auditTaskID, terminalTaskID), guard.Actor, reason, now); err != nil {
				return store.WorkflowActionResult{}, err
			}
			return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionApplied)
		}
		if status == store.TeamWorkflowStatusFailing || status == store.TeamWorkflowStatusFailed {
			return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionAlreadyApplied)
		}
		return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionConflict)

	case store.WorkflowActionRetryExpansion:
		liveClaim := expansionToken != nil && expansionLeaseValue.Valid && expansionLeaseValue.Time.After(now)
		if status != store.TeamWorkflowStatusPendingExpansion || !statusOK || liveClaim {
			return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionConflict)
		}
		alreadyDue := expansionToken == nil && nextExpansionValue.Valid && !nextExpansionValue.Time.After(now)
		if alreadyDue {
			return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionAlreadyApplied)
		}
		// Re-arm: clear any expired claim and pull the next expansion forward to
		// now. Attempt count / last error and the bounded budget are preserved.
		res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET expansion_token=NULL,expansion_lease_until=NULL,next_expansion_at=?,updated_at=?
			WHERE id=? AND tenant_id=? AND team_id=? AND status=? AND plan_revision=?
			AND NOT (expansion_token IS NOT NULL AND expansion_lease_until IS NOT NULL AND expansion_lease_until>?)`,
			now, now, guard.WorkflowID, tid, guard.TeamID, store.TeamWorkflowStatusPendingExpansion, revision, now)
		if err != nil {
			return store.WorkflowActionResult{}, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return store.WorkflowActionResult{}, err
		}
		if n != 1 {
			var (
				curStatus   string
				curRevision int
				curToken    *uuid.UUID
				curNext     *time.Time
			)
			err := tx.QueryRowContext(ctx, `SELECT status,plan_revision,expansion_token,next_expansion_at
				FROM team_workflows WHERE id=? AND tenant_id=? AND team_id=?`,
				guard.WorkflowID, tid, guard.TeamID).Scan(&curStatus, &curRevision, &curToken, &curNext)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return store.WorkflowActionResult{}, err
			}
			if err == nil && curRevision == revision && curStatus == store.TeamWorkflowStatusPendingExpansion && curToken == nil && curNext != nil && !curNext.After(now) {
				return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionAlreadyApplied)
			}
			return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionConflict)
		}
		if err := insertSQLiteActionComment(ctx, tx, tid, cancelCommentTarget(auditTaskID, terminalTaskID), guard.Actor, reason, now); err != nil {
			return store.WorkflowActionResult{}, err
		}
		return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionApplied)

	case store.WorkflowActionRetryDelivery:
		terminal := status == store.TeamWorkflowStatusCompleted || status == store.TeamWorkflowStatusFailed || status == store.TeamWorkflowStatusCancelled
		liveClaim := deliveryToken != nil && deliveryLeaseValue.Valid && deliveryLeaseValue.Time.After(now)
		if !terminal || !statusOK || deliveredAtValue.Valid || liveClaim {
			return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionConflict)
		}
		switch deliveryStatus {
		case store.TeamWorkflowDeliveryDead:
			// Start a fresh bounded manual delivery cycle. last_delivery_error is
			// preserved until the next attempt overwrites it.
			res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET delivery_status=?,delivery_attempt_count=0,next_delivery_at=?,
				delivery_token=NULL,delivery_lease_until=NULL,updated_at=?
				WHERE id=? AND tenant_id=? AND team_id=? AND status=? AND plan_revision=?
				AND delivery_status=? AND delivered_at IS NULL
				AND NOT (delivery_token IS NOT NULL AND delivery_lease_until IS NOT NULL AND delivery_lease_until>?)`,
				store.TeamWorkflowDeliveryPending, now, now, guard.WorkflowID, tid, guard.TeamID, status, revision, store.TeamWorkflowDeliveryDead, now)
			if err != nil {
				return store.WorkflowActionResult{}, err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return store.WorkflowActionResult{}, err
			}
			if n != 1 {
				var (
					curStatus      string
					curRevision    int
					curDelivStatus string
				)
				err := tx.QueryRowContext(ctx, `SELECT status,plan_revision,delivery_status FROM team_workflows
					WHERE id=? AND tenant_id=? AND team_id=?`, guard.WorkflowID, tid, guard.TeamID).
					Scan(&curStatus, &curRevision, &curDelivStatus)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return store.WorkflowActionResult{}, err
				}
				if err == nil && curStatus == status && curRevision == revision && curDelivStatus == store.TeamWorkflowDeliveryPending {
					return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionAlreadyApplied)
				}
				return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionConflict)
			}
			if err := insertSQLiteActionComment(ctx, tx, tid, deliveryCommentTarget(terminalTaskID, auditTaskID), guard.Actor, reason, now); err != nil {
				return store.WorkflowActionResult{}, err
			}
			return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionApplied)
		case store.TeamWorkflowDeliveryPending:
			if nextDeliveryValue.Valid && !nextDeliveryValue.Time.After(now) {
				return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionAlreadyApplied)
			}
			return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionConflict)
		default:
			return s.finishSQLiteAction(ctx, tx, tid, guard, store.WorkflowActionConflict)
		}
	}
	return store.WorkflowActionResult{}, store.ErrWorkflowActionInvalid
}

// finishSQLiteAction reloads the authoritative workflow + tasks inside the open
// transaction and commits, returning the typed result. It is used for every
// terminal outcome (Applied/AlreadyApplied/Conflict) so the caller always
// receives the current post-state to reconcile against.
func (s *SQLiteTeamStore) finishSQLiteAction(ctx context.Context, tx *sql.Tx, tid uuid.UUID, guard store.WorkflowActionGuard, outcome store.WorkflowActionOutcome) (store.WorkflowActionResult, error) {
	w, err := scanSQLiteWorkflow(tx.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE id=? AND tenant_id=? AND team_id=?`, guard.WorkflowID, tid, guard.TeamID))
	if err != nil {
		return store.WorkflowActionResult{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+taskSelectCols+` `+taskJoinClause+` WHERE t.workflow_id=? AND t.tenant_id=? AND t.team_id=? ORDER BY t.task_number`, guard.WorkflowID, tid, guard.TeamID)
	if err != nil {
		return store.WorkflowActionResult{}, err
	}
	tasks, err := scanTaskRowsJoined(rows)
	rows.Close()
	if err != nil {
		return store.WorkflowActionResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return store.WorkflowActionResult{}, err
	}
	return store.WorkflowActionResult{Outcome: outcome, Action: guard.Action, Workflow: w, Tasks: tasks}, nil
}

// insertSQLiteActionComment records the operator/coordinator justification as a
// "recovery" comment attributed to the correct author (AgentID for a coordinator
// tool run, UserID for an admin RPC). It is a no-op when there is no target task
// or the reason is empty, and is only ever called on an Applied outcome.
func insertSQLiteActionComment(ctx context.Context, tx *sql.Tx, tid uuid.UUID, taskID *uuid.UUID, actor store.WorkflowActionActor, content string, now time.Time) error {
	if taskID == nil || *taskID == uuid.Nil || content == "" {
		return nil
	}
	var agentID *uuid.UUID
	var userID string
	if actor.Kind == store.WorkflowActorCoordinator {
		agentID = actor.AgentID
	} else {
		userID = actor.UserID
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO team_task_comments (id, task_id, agent_id, user_id, content, comment_type, created_at, tenant_id)
		VALUES (?,?,?,?,?,?,?,?)`,
		store.GenNewID(), *taskID, agentID, sql.NullString{String: userID, Valid: userID != ""}, content, "recovery", now, tid); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET comment_count=comment_count+1 WHERE id=? AND tenant_id=?`, *taskID, tid)
	if err != nil {
		return err
	}
	return requireOneSQLiteWorkflowActionRow(res)
}

func requireOneSQLiteWorkflowActionRow(result sql.Result) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("workflow action update affected %d rows", n)
	}
	return nil
}

// cancelCommentTarget picks the audit task (falling back to the terminal task)
// for workflow-level action comments that are not step-scoped.
func cancelCommentTarget(auditTaskID, terminalTaskID *uuid.UUID) *uuid.UUID {
	if auditTaskID != nil && *auditTaskID != uuid.Nil {
		return auditTaskID
	}
	return terminalTaskID
}

// deliveryCommentTarget prefers the terminal task (the delivered result) for a
// delivery-retry comment, falling back to the audit task.
func deliveryCommentTarget(terminalTaskID, auditTaskID *uuid.UUID) *uuid.UUID {
	if terminalTaskID != nil && *terminalTaskID != uuid.Nil {
		return terminalTaskID
	}
	return auditTaskID
}

package pg

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

// This file implements ApplyWorkflowAction — the single authoritative store
// transition for the six operator/coordinator recovery actions that carry no
// backend-built plan (retry_blocked, request_revision, cancel_workflow,
// fail_workflow, retry_expansion, retry_delivery). apply_replan is handled
// separately via CommitWorkflowReplan because it must commit a backend-frozen
// task list. Every branch here opens one transaction, locks the authoritative
// workflow row with FOR UPDATE (and the target task for step-scoped actions),
// enforces the optimistic ExpectedStatus/ExpectedPlanRevision/ExpectedTaskStatus
// guards, decides Applied vs AlreadyApplied vs Conflict atomically, mutates at
// most once, writes the actor-attributed comment only on Applied, then reloads
// the authoritative workflow + tasks before commit. The SQLite twin in
// internal/store/sqlitestore/team_workflows_actions.go uses the identical
// predicate/outcome logic.

// ApplyWorkflowAction is the PG implementation of the shared action contract.
func (s *PGTeamStore) ApplyWorkflowAction(ctx context.Context, guard store.WorkflowActionGuard) (store.WorkflowActionResult, error) {
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

	// Lock and read the authoritative workflow row. Tenant + team scope come from
	// context/guard; a caller can never act across tenants by forging a field.
	var (
		status          string
		revision        int
		deliveryStatus  string
		deliveredAt     *time.Time
		deliveryToken   *uuid.UUID
		deliveryLease   *time.Time
		expansionToken  *uuid.UUID
		expansionLease  *time.Time
		nextExpansionAt *time.Time
		auditTaskID     *uuid.UUID
		terminalTaskID  *uuid.UUID
		nextDeliveryAt  *time.Time
	)
	err = tx.QueryRowContext(ctx, `SELECT status,plan_revision,delivery_status,delivered_at,delivery_token,delivery_lease_until,
		expansion_token,expansion_lease_until,next_expansion_at,audit_task_id,terminal_task_id,next_delivery_at
		FROM team_workflows WHERE id=$1 AND tenant_id=$2 AND team_id=$3 FOR UPDATE`,
		guard.WorkflowID, tid, guard.TeamID).Scan(&status, &revision, &deliveryStatus, &deliveredAt, &deliveryToken, &deliveryLease,
		&expansionToken, &expansionLease, &nextExpansionAt, &auditTaskID, &terminalTaskID, &nextDeliveryAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return store.WorkflowActionResult{}, store.ErrTaskNotFound
		}
		return store.WorkflowActionResult{}, err
	}

	// Global optimistic revision guard: a stale caller (or a revision that moved
	// under a concurrent replan) reconciles against a fresh fetch.
	if guard.ExpectedPlanRevision != revision {
		return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionConflict)
	}
	statusOK := guard.ExpectedStatus == status

	// Step-scoped actions lock and read the target task too.
	var (
		taskStatus string
		taskKind   string
		taskRev    int
		taskFound  bool
	)
	if guard.Action.StepScoped() {
		terr := tx.QueryRowContext(ctx, `SELECT status,COALESCE(workflow_kind,''),COALESCE(plan_revision,1)
			FROM team_tasks WHERE id=$1 AND team_id=$2 AND tenant_id=$3 AND workflow_id=$4 FOR UPDATE`,
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
			res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=$1,blocker_reason='',recovery_count=recovery_count+1,
				dispatch_token=NULL,dispatch_lease_until=NULL,locked_at=NULL,lock_expires_at=NULL,
				escalation_status=$2,escalation_attempt_count=0,escalation_next_at=NULL,escalation_last_error='',updated_at=$3
				WHERE id=$4 AND team_id=$5 AND tenant_id=$6 AND workflow_id=$7 AND workflow_kind=$8 AND plan_revision=$9 AND status=$10`,
				store.TeamTaskStatusPending, store.TeamTaskEscalationDelivered, now,
				*guard.TaskID, guard.TeamID, tid, guard.WorkflowID, store.TeamWorkflowTaskKindWork, revision, store.TeamTaskStatusBlocked)
			if err != nil {
				return store.WorkflowActionResult{}, err
			}
			if err := requireOneWorkflowActionRow(res); err != nil {
				return store.WorkflowActionResult{}, err
			}
			// needs_revision → running so the paused old-plan pending tasks resume.
			if status == store.TeamWorkflowStatusNeedsRevision {
				res, err = tx.ExecContext(ctx, `UPDATE team_workflows SET status=$1,updated_at=$2 WHERE id=$3 AND tenant_id=$4 AND team_id=$5 AND status=$6 AND plan_revision=$7`,
					store.TeamWorkflowStatusRunning, now, guard.WorkflowID, tid, guard.TeamID, store.TeamWorkflowStatusNeedsRevision, revision)
				if err != nil {
					return store.WorkflowActionResult{}, err
				}
				if err := requireOneWorkflowActionRow(res); err != nil {
					return store.WorkflowActionResult{}, err
				}
			}
			if err := insertPGActionComment(ctx, tx, tid, guard.TaskID, guard.Actor, reason, now); err != nil {
				return store.WorkflowActionResult{}, err
			}
			return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionApplied)
		}
		// Idempotent replay: the blocked task is already pending at the same
		// revision and the workflow is running again.
		if taskFound && taskRev == revision && taskStatus == store.TeamTaskStatusPending && status == store.TeamWorkflowStatusRunning {
			return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionAlreadyApplied)
		}
		return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionConflict)

	case store.WorkflowActionRequestRevision:
		canApply := taskFound && taskKind == store.TeamWorkflowTaskKindWork && taskRev == revision &&
			taskStatus == store.TeamTaskStatusBlocked && status == store.TeamWorkflowStatusRunning
		if canApply && statusOK && taskStatusOK {
			// Pause the current plan: running → needs_revision. Keep the selected
			// blocker + its durable escalation untouched; only return the current-
			// revision dispatching|in_progress tasks to pending (tokens cleared).
			res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=$1,updated_at=$2 WHERE id=$3 AND tenant_id=$4 AND team_id=$5 AND status=$6 AND plan_revision=$7`,
				store.TeamWorkflowStatusNeedsRevision, now, guard.WorkflowID, tid, guard.TeamID, store.TeamWorkflowStatusRunning, revision)
			if err != nil {
				return store.WorkflowActionResult{}, err
			}
			if err := requireOneWorkflowActionRow(res); err != nil {
				return store.WorkflowActionResult{}, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=$1,dispatch_token=NULL,dispatch_lease_until=NULL,locked_at=NULL,lock_expires_at=NULL,updated_at=$2
				WHERE workflow_id=$3 AND tenant_id=$4 AND team_id=$5 AND workflow_kind=$6 AND plan_revision=$7 AND status IN ($8,$9)`,
				store.TeamTaskStatusPending, now, guard.WorkflowID, tid, guard.TeamID, store.TeamWorkflowTaskKindWork, revision,
				store.TeamTaskStatusDispatching, store.TeamTaskStatusInProgress); err != nil {
				return store.WorkflowActionResult{}, err
			}
			if err := insertPGActionComment(ctx, tx, tid, guard.TaskID, guard.Actor, reason, now); err != nil {
				return store.WorkflowActionResult{}, err
			}
			return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionApplied)
		}
		if taskFound && taskRev == revision && taskStatus == store.TeamTaskStatusBlocked && status == store.TeamWorkflowStatusNeedsRevision {
			return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionAlreadyApplied)
		}
		return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionConflict)

	case store.WorkflowActionCancelWorkflow:
		canApply := status == store.TeamWorkflowStatusPendingExpansion || status == store.TeamWorkflowStatusRunning || status == store.TeamWorkflowStatusNeedsRevision
		if canApply && statusOK {
			res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=$1,cancel_reason=$2,cancelled_at=$3,
				expansion_token=NULL,expansion_lease_until=NULL,finalize_token=NULL,finalize_lease_until=NULL,updated_at=$3
				WHERE id=$4 AND tenant_id=$5 AND team_id=$6 AND status=$7 AND plan_revision=$8`,
				store.TeamWorkflowStatusCancelling, reason, now, guard.WorkflowID, tid, guard.TeamID, status, revision)
			if err != nil {
				return store.WorkflowActionResult{}, err
			}
			if err := requireOneWorkflowActionRow(res); err != nil {
				return store.WorkflowActionResult{}, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=$1,result=COALESCE(result,$2),dispatch_token=NULL,dispatch_lease_until=NULL,locked_at=NULL,lock_expires_at=NULL,updated_at=$3
				WHERE workflow_id=$4 AND tenant_id=$5 AND team_id=$6 AND workflow_kind=$7 AND status NOT IN ($8,$9,$10,$11)`,
				store.TeamTaskStatusCancelled, "Cancelled: "+reason, now, guard.WorkflowID, tid, guard.TeamID, store.TeamWorkflowTaskKindWork,
				store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled, store.TeamTaskStatusStale); err != nil {
				return store.WorkflowActionResult{}, err
			}
			if err := insertPGActionComment(ctx, tx, tid, cancelCommentTarget(auditTaskID, terminalTaskID), guard.Actor, reason, now); err != nil {
				return store.WorkflowActionResult{}, err
			}
			return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionApplied)
		}
		if status == store.TeamWorkflowStatusCancelling || status == store.TeamWorkflowStatusCancelled {
			return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionAlreadyApplied)
		}
		return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionConflict)

	case store.WorkflowActionFailWorkflow:
		canApply := status == store.TeamWorkflowStatusRunning || status == store.TeamWorkflowStatusNeedsRevision
		if canApply && statusOK {
			settleDeadline := now.Add(store.WorkflowFailureSettleDelay)
			res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=$1,failure_summary=CASE WHEN failure_summary='' THEN $2 ELSE failure_summary END,
				failure_settle_deadline=COALESCE(failure_settle_deadline,$3),finalize_token=NULL,finalize_lease_until=NULL,updated_at=$4
				WHERE id=$5 AND tenant_id=$6 AND team_id=$7 AND status=$8 AND plan_revision=$9`,
				store.TeamWorkflowStatusFailing, reason, settleDeadline, now, guard.WorkflowID, tid, guard.TeamID, status, revision)
			if err != nil {
				return store.WorkflowActionResult{}, err
			}
			if err := requireOneWorkflowActionRow(res); err != nil {
				return store.WorkflowActionResult{}, err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=$1,result=COALESCE(result,$2),dispatch_token=NULL,dispatch_lease_until=NULL,locked_at=NULL,lock_expires_at=NULL,updated_at=$3
				WHERE workflow_id=$4 AND tenant_id=$5 AND team_id=$6 AND workflow_kind=$7 AND status NOT IN ($8,$9,$10,$11)`,
				store.TeamTaskStatusCancelled, "Cancelled because workflow failed", now, guard.WorkflowID, tid, guard.TeamID, store.TeamWorkflowTaskKindWork,
				store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled, store.TeamTaskStatusStale); err != nil {
				return store.WorkflowActionResult{}, err
			}
			if err := insertPGActionComment(ctx, tx, tid, cancelCommentTarget(auditTaskID, terminalTaskID), guard.Actor, reason, now); err != nil {
				return store.WorkflowActionResult{}, err
			}
			return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionApplied)
		}
		if status == store.TeamWorkflowStatusFailing || status == store.TeamWorkflowStatusFailed {
			return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionAlreadyApplied)
		}
		return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionConflict)

	case store.WorkflowActionRetryExpansion:
		liveClaim := expansionToken != nil && expansionLease != nil && expansionLease.After(now)
		if status != store.TeamWorkflowStatusPendingExpansion || !statusOK || liveClaim {
			return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionConflict)
		}
		alreadyDue := expansionToken == nil && nextExpansionAt != nil && !nextExpansionAt.After(now)
		if alreadyDue {
			return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionAlreadyApplied)
		}
		// Re-arm: clear any expired claim and pull the next expansion forward to
		// now. Attempt count / last error and the bounded budget are preserved.
		res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET expansion_token=NULL,expansion_lease_until=NULL,next_expansion_at=$1,updated_at=$1
			WHERE id=$2 AND tenant_id=$3 AND team_id=$4 AND status=$5 AND plan_revision=$6
			AND NOT (expansion_token IS NOT NULL AND expansion_lease_until IS NOT NULL AND expansion_lease_until>$1)`,
			now, guard.WorkflowID, tid, guard.TeamID, store.TeamWorkflowStatusPendingExpansion, revision)
		if err != nil {
			return store.WorkflowActionResult{}, err
		}
		if err := requireOneWorkflowActionRow(res); err != nil {
			return store.WorkflowActionResult{}, err
		}
		if err := insertPGActionComment(ctx, tx, tid, cancelCommentTarget(auditTaskID, terminalTaskID), guard.Actor, reason, now); err != nil {
			return store.WorkflowActionResult{}, err
		}
		return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionApplied)

	case store.WorkflowActionRetryDelivery:
		terminal := status == store.TeamWorkflowStatusCompleted || status == store.TeamWorkflowStatusFailed || status == store.TeamWorkflowStatusCancelled
		liveClaim := deliveryToken != nil && deliveryLease != nil && deliveryLease.After(now)
		if !terminal || !statusOK || deliveredAt != nil || liveClaim {
			return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionConflict)
		}
		switch deliveryStatus {
		case store.TeamWorkflowDeliveryDead:
			// Start a fresh bounded manual delivery cycle. last_delivery_error is
			// preserved until the next attempt overwrites it.
			res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET delivery_status=$1,delivery_attempt_count=0,next_delivery_at=$2,
				delivery_token=NULL,delivery_lease_until=NULL,updated_at=$2
				WHERE id=$3 AND tenant_id=$4 AND team_id=$5 AND status=$6 AND plan_revision=$7
				AND delivery_status=$8 AND delivered_at IS NULL
				AND NOT (delivery_token IS NOT NULL AND delivery_lease_until IS NOT NULL AND delivery_lease_until>$2)`,
				store.TeamWorkflowDeliveryPending, now, guard.WorkflowID, tid, guard.TeamID, status, revision, store.TeamWorkflowDeliveryDead)
			if err != nil {
				return store.WorkflowActionResult{}, err
			}
			if err := requireOneWorkflowActionRow(res); err != nil {
				return store.WorkflowActionResult{}, err
			}
			if err := insertPGActionComment(ctx, tx, tid, deliveryCommentTarget(terminalTaskID, auditTaskID), guard.Actor, reason, now); err != nil {
				return store.WorkflowActionResult{}, err
			}
			return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionApplied)
		case store.TeamWorkflowDeliveryPending:
			if nextDeliveryAt != nil && !nextDeliveryAt.After(now) {
				return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionAlreadyApplied)
			}
			return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionConflict)
		default:
			return s.finishPGAction(ctx, tx, tid, guard, store.WorkflowActionConflict)
		}
	}
	return store.WorkflowActionResult{}, store.ErrWorkflowActionInvalid
}

// finishPGAction reloads the authoritative workflow + tasks inside the open
// transaction and commits, returning the typed result. It is used for every
// terminal outcome (Applied/AlreadyApplied/Conflict) so the caller always
// receives the current post-state to reconcile against.
func (s *PGTeamStore) finishPGAction(ctx context.Context, tx *sql.Tx, tid uuid.UUID, guard store.WorkflowActionGuard, outcome store.WorkflowActionOutcome) (store.WorkflowActionResult, error) {
	w, err := scanPGWorkflow(tx.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE id=$1 AND tenant_id=$2 AND team_id=$3`, guard.WorkflowID, tid, guard.TeamID))
	if err != nil {
		return store.WorkflowActionResult{}, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+taskSelectCols+` `+taskJoinClause+` WHERE t.workflow_id=$1 AND t.tenant_id=$2 AND t.team_id=$3 ORDER BY t.task_number`, guard.WorkflowID, tid, guard.TeamID)
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

// insertPGActionComment records the operator/coordinator justification as a
// "recovery" comment attributed to the correct author (AgentID for a coordinator
// tool run, UserID for an admin RPC). It is a no-op when there is no target task
// or the reason is empty, and is only ever called on an Applied outcome.
func insertPGActionComment(ctx context.Context, tx *sql.Tx, tid uuid.UUID, taskID *uuid.UUID, actor store.WorkflowActionActor, content string, now time.Time) error {
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
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		store.GenNewID(), *taskID, agentID, sql.NullString{String: userID, Valid: userID != ""}, content, "recovery", now, tid); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE team_tasks SET comment_count=comment_count+1 WHERE id=$1 AND tenant_id=$2`, *taskID, tid)
	if err != nil {
		return err
	}
	return requireOneWorkflowActionRow(res)
}

func requireOneWorkflowActionRow(result sql.Result) error {
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

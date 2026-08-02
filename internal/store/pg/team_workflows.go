package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const workflowSelectCols = `id, team_id, tenant_id, status, canonical_plan, schema_version, plan_hash,
	coordinator_agent_id, coordinator_agent_key, origin_agent_id, origin_agent_key, origin_run_id,
	origin_session_key, origin_channel, origin_chat_id, origin_peer_kind, origin_local_key, origin_user_id, origin_sender_id, origin_role, origin_routing,
	auto_expand, audit_task_id, terminal_task_id, expansion_token, expansion_lease_until,
	finalize_token, finalize_lease_until, finalize_claimed_at, finalized_at, failure_settle_deadline,
	failure_summary, result_summary, delivery_status, delivery_token, delivery_lease_until, delivered_at, created_at, updated_at,
	plan_revision, expansion_attempt_count, next_expansion_at, last_expansion_error,
	delivery_attempt_count, next_delivery_at, last_delivery_error, cancel_reason, cancelled_at, classification_audit_id`

func (s *PGTeamStore) CreateWorkflowWithTasks(ctx context.Context, workflow *store.TeamWorkflowData, tasks []store.TeamTaskData) error {
	if len(tasks) == 0 {
		return fmt.Errorf("workflow tasks are required")
	}
	return s.createWorkflow(ctx, workflow, tasks, nil)
}

func (s *PGTeamStore) CreatePendingWorkflowRequest(ctx context.Context, workflow *store.TeamWorkflowData, auditTask *store.TeamTaskData) error {
	if auditTask == nil {
		return fmt.Errorf("workflow audit task is required")
	}
	if auditTask.OwnerAgentID == nil || *auditTask.OwnerAgentID != workflow.CoordinatorAgentID {
		return fmt.Errorf("workflow audit owner must match canonical coordinator")
	}
	return s.createWorkflow(ctx, workflow, nil, auditTask)
}

func (s *PGTeamStore) createWorkflow(ctx context.Context, workflow *store.TeamWorkflowData, tasks []store.TeamTaskData, auditTask *store.TeamTaskData) error {
	if err := prepareWorkflow(ctx, workflow); err != nil {
		return err
	}
	if err := validateWorkflowTaskScope(workflow, tasks, auditTask); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := insertPGWorkflow(ctx, tx, workflow); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM agent_teams WHERE id = $1 FOR UPDATE`, workflow.TeamID); err != nil {
		return fmt.Errorf("lock workflow team: %w", err)
	}
	if auditTask != nil {
		auditTask.WorkflowID = &workflow.ID
		auditTask.WorkflowKind = store.TeamWorkflowTaskKindAudit
		auditTask.WorkflowStepID = ""
		auditTask.WorkflowTerminal = false
		if err := insertPGWorkflowTask(ctx, tx, auditTask); err != nil {
			return err
		}
		workflow.AuditTaskID = &auditTask.ID
	}
	for i := range tasks {
		tasks[i].WorkflowID = &workflow.ID
		tasks[i].WorkflowKind = store.TeamWorkflowTaskKindWork
		if err := insertPGWorkflowTask(ctx, tx, &tasks[i]); err != nil {
			return err
		}
		if tasks[i].WorkflowTerminal {
			id := tasks[i].ID
			workflow.TerminalTaskID = &id
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET audit_task_id=$1, terminal_task_id=$2, updated_at=$3 WHERE id=$4 AND tenant_id=$5`,
		workflow.AuditTaskID, workflow.TerminalTaskID, time.Now(), workflow.ID, workflow.TenantID); err != nil {
		return err
	}
	return tx.Commit()
}

func validateWorkflowTaskScope(workflow *store.TeamWorkflowData, tasks []store.TeamTaskData, auditTask *store.TeamTaskData) error {
	validateTask := func(task *store.TeamTaskData) error {
		if task.TeamID != uuid.Nil && task.TeamID != workflow.TeamID {
			return fmt.Errorf("workflow task team mismatch")
		}
		if task.TenantID != uuid.Nil && task.TenantID != workflow.TenantID {
			return fmt.Errorf("workflow task tenant mismatch")
		}
		task.TeamID = workflow.TeamID
		task.TenantID = workflow.TenantID
		return nil
	}
	if auditTask != nil {
		if err := validateTask(auditTask); err != nil {
			return err
		}
	}
	for i := range tasks {
		if err := validateTask(&tasks[i]); err != nil {
			return err
		}
	}
	return nil
}

func prepareWorkflow(ctx context.Context, workflow *store.TeamWorkflowData) error {
	if workflow == nil {
		return fmt.Errorf("workflow is required")
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required")
	}
	if workflow.TenantID != uuid.Nil && workflow.TenantID != tid {
		return fmt.Errorf("workflow tenant mismatch")
	}
	workflow.TenantID = tid
	if workflow.ID == uuid.Nil {
		workflow.ID = store.GenNewID()
	}
	if workflow.TeamID == uuid.Nil || workflow.CoordinatorAgentID == uuid.Nil || workflow.OriginAgentID == uuid.Nil {
		return fmt.Errorf("workflow team, coordinator, and origin agent are required")
	}
	if strings.TrimSpace(workflow.OriginRunID) == "" || strings.TrimSpace(workflow.PlanHash) == "" {
		return fmt.Errorf("workflow origin_run_id and plan_hash are required")
	}
	if workflow.Status == "" {
		workflow.Status = store.TeamWorkflowStatusRunning
	}
	if len(workflow.OriginRouting) == 0 {
		workflow.OriginRouting = json.RawMessage(`{}`)
	}
	now := time.Now()
	workflow.CreatedAt = now
	workflow.UpdatedAt = now
	return nil
}

func insertPGWorkflow(ctx context.Context, tx *sql.Tx, w *store.TeamWorkflowData) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO team_workflows (
		id, team_id, tenant_id, status, canonical_plan, schema_version, plan_hash,
		coordinator_agent_id, coordinator_agent_key, origin_agent_id, origin_agent_key, origin_run_id,
		origin_session_key, origin_channel, origin_chat_id, origin_peer_kind, origin_local_key, origin_user_id, origin_sender_id, origin_role, origin_routing,
		auto_expand, failure_summary, result_summary, classification_audit_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)`,
		w.ID, w.TeamID, w.TenantID, w.Status, w.CanonicalPlan, w.SchemaVersion, w.PlanHash,
		w.CoordinatorAgentID, w.CoordinatorAgentKey, w.OriginAgentID, w.OriginAgentKey, w.OriginRunID,
		w.OriginSessionKey, w.OriginChannel, w.OriginChatID, w.OriginPeerKind, w.OriginLocalKey, w.OriginUserID, w.OriginSenderID, w.OriginRole, w.OriginRouting,
		w.AutoExpand, w.FailureSummary, w.ResultSummary, w.ClassificationAuditID, w.CreatedAt, w.UpdatedAt)
	return err
}

func insertPGWorkflowTask(ctx context.Context, tx *sql.Tx, task *store.TeamTaskData) error {
	if task.ID == uuid.Nil {
		task.ID = store.GenNewID()
	}
	if task.TaskType == "" {
		task.TaskType = "general"
	}
	if task.Status == "" {
		task.Status = store.TeamTaskStatusPending
	}
	var taskNumber int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(task_number),0)+1 FROM team_tasks WHERE team_id=$1 AND COALESCE(chat_id,'')=$2`, task.TeamID, task.ChatID).Scan(&taskNumber); err != nil {
		return err
	}
	task.TaskNumber = taskNumber
	hex := strings.ReplaceAll(task.ID.String(), "-", "")
	task.Identifier = fmt.Sprintf("T-%03d-%s", taskNumber, hex[len(hex)-4:])
	// Promote a legacy metadata dispatch_count seed into the durable column
	// (000098) before insert; the column is what the atomic claim increments.
	task.SeedDispatchCountFromMetadata()
	meta := []byte(`{}`)
	if len(task.Metadata) > 0 {
		meta, _ = json.Marshal(task.Metadata)
	}
	now := time.Now()
	task.CreatedAt, task.UpdatedAt = now, now
	_, err := tx.ExecContext(ctx, `INSERT INTO team_tasks (
		id,team_id,tenant_id,subject,description,status,owner_agent_id,blocked_by,priority,result,user_id,channel,
		task_type,task_number,identifier,created_by_agent_id,parent_id,chat_id,metadata,
		workflow_id,workflow_step_id,workflow_kind,workflow_terminal,plan_revision,dispatch_count,created_at,updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)`,
		task.ID, task.TeamID, tenantIDForInsert(ctx), task.Subject, task.Description, task.Status,
		task.OwnerAgentID, pq.Array(task.BlockedBy), task.Priority, task.Result,
		sql.NullString{String: task.UserID, Valid: task.UserID != ""}, sql.NullString{String: task.Channel, Valid: task.Channel != ""},
		task.TaskType, task.TaskNumber, task.Identifier, task.CreatedByAgentID, task.ParentID,
		sql.NullString{String: task.ChatID, Valid: task.ChatID != ""}, meta,
		task.WorkflowID, sql.NullString{String: task.WorkflowStepID, Valid: task.WorkflowStepID != ""}, task.WorkflowKind,
		task.WorkflowTerminal, store.PlanRevisionOrDefault(task.PlanRevision), task.DispatchCount, now, now)
	return err
}

func (s *PGTeamStore) GetWorkflow(ctx context.Context, workflowID uuid.UUID) (*store.TeamWorkflowData, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE id=$1 AND tenant_id=$2`, workflowID, tenantIDForInsert(ctx))
	return scanPGWorkflow(row)
}

func (s *PGTeamStore) FindWorkflowByCreationKey(ctx context.Context, teamID uuid.UUID, originRunID, planHash string) (*store.TeamWorkflowData, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE tenant_id=$1 AND team_id=$2 AND origin_run_id=$3 AND plan_hash=$4`, tenantIDForInsert(ctx), teamID, originRunID, planHash)
	return scanPGWorkflow(row)
}

type workflowRowScanner interface{ Scan(...any) error }

func scanPGWorkflow(row workflowRowScanner) (*store.TeamWorkflowData, error) {
	var w store.TeamWorkflowData
	if err := row.Scan(&w.ID, &w.TeamID, &w.TenantID, &w.Status, &w.CanonicalPlan, &w.SchemaVersion, &w.PlanHash,
		&w.CoordinatorAgentID, &w.CoordinatorAgentKey, &w.OriginAgentID, &w.OriginAgentKey, &w.OriginRunID,
		&w.OriginSessionKey, &w.OriginChannel, &w.OriginChatID, &w.OriginPeerKind, &w.OriginLocalKey, &w.OriginUserID, &w.OriginSenderID, &w.OriginRole, &w.OriginRouting,
		&w.AutoExpand, &w.AuditTaskID, &w.TerminalTaskID, &w.ExpansionToken, &w.ExpansionLeaseUntil,
		&w.FinalizeToken, &w.FinalizeLeaseUntil, &w.FinalizeClaimedAt, &w.FinalizedAt, &w.FailureSettleDeadline,
		&w.FailureSummary, &w.ResultSummary, &w.DeliveryStatus, &w.DeliveryToken, &w.DeliveryLeaseUntil, &w.DeliveredAt, &w.CreatedAt, &w.UpdatedAt,
		&w.PlanRevision, &w.ExpansionAttemptCount, &w.NextExpansionAt, &w.LastExpansionError,
		&w.DeliveryAttemptCount, &w.NextDeliveryAt, &w.LastDeliveryError, &w.CancelReason, &w.CancelledAt, &w.ClassificationAuditID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrTaskNotFound
		}
		return nil, err
	}
	return &w, nil
}

func (s *PGTeamStore) ListWorkflowTasks(ctx context.Context, workflowID uuid.UUID) ([]store.TeamTaskData, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskSelectCols+` `+taskJoinClause+` JOIN team_workflows w ON w.id=t.workflow_id AND w.tenant_id=t.tenant_id AND w.team_id=t.team_id WHERE t.workflow_id=$1 AND t.tenant_id=$2 ORDER BY t.task_number`, workflowID, tenantIDForInsert(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

func (s *PGTeamStore) ClaimPendingWorkflowExpansion(ctx context.Context, workflowID, coordinatorID uuid.UUID, leaseUntil time.Time) (uuid.UUID, error) {
	token := uuid.New()
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE team_workflows SET expansion_token=$1,expansion_lease_until=$2,updated_at=$3 WHERE id=$4 AND tenant_id=$5 AND coordinator_agent_id=$6 AND status=$7 AND (expansion_token IS NULL OR expansion_lease_until<$3)`, token, leaseUntil, now, workflowID, tenantIDForInsert(ctx), coordinatorID, store.TeamWorkflowStatusPendingExpansion)
	if err != nil {
		return uuid.Nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return uuid.Nil, fmt.Errorf("pending workflow expansion is already claimed")
	}
	return token, nil
}

func (s *PGTeamStore) ExpandPendingWorkflow(ctx context.Context, workflowID, coordinatorID, expansionToken uuid.UUID, tasks []store.TeamTaskData) error {
	return s.expandPendingWorkflow(ctx, workflowID, uuid.Nil, coordinatorID, expansionToken, tasks)
}

func (s *PGTeamStore) ApprovePendingWorkflowRequest(ctx context.Context, workflowID, auditTaskID uuid.UUID, actor store.WorkflowApprovalActor, tasks []store.TeamTaskData) error {
	workflow, err := s.GetWorkflow(ctx, workflowID)
	if err != nil {
		return err
	}
	if err := authorizeWorkflowApproval(actor, workflow.CoordinatorAgentID); err != nil {
		return err
	}
	token, err := s.ClaimPendingWorkflowExpansion(ctx, workflowID, workflow.CoordinatorAgentID, time.Now().Add(2*time.Minute))
	if err != nil {
		return err
	}
	return s.expandPendingWorkflow(ctx, workflowID, auditTaskID, workflow.CoordinatorAgentID, token, tasks)
}

func authorizeWorkflowApproval(actor store.WorkflowApprovalActor, coordinatorID uuid.UUID) error {
	if actor.AgentID != nil {
		if *actor.AgentID == coordinatorID {
			return nil
		}
		return fmt.Errorf("workflow approval requires the canonical team lead")
	}
	role := strings.ToLower(strings.TrimSpace(actor.Role))
	if strings.TrimSpace(actor.UserID) != "" && (role == "owner" || role == "admin" || role == "operator") {
		return nil
	}
	return fmt.Errorf("workflow approval requires an authorized dashboard actor")
}

func (s *PGTeamStore) expandPendingWorkflow(ctx context.Context, workflowID, auditTaskID, coordinatorID, expansionToken uuid.UUID, tasks []store.TeamTaskData) error {
	if len(tasks) == 0 {
		return fmt.Errorf("workflow tasks are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	var teamID uuid.UUID
	var storedAuditID *uuid.UUID
	if err := tx.QueryRowContext(ctx, `SELECT team_id,audit_task_id FROM team_workflows WHERE id=$1 AND tenant_id=$2 AND status=$3 AND coordinator_agent_id=$4 AND expansion_token=$5 FOR UPDATE`,
		workflowID, tid, store.TeamWorkflowStatusPendingExpansion, coordinatorID, expansionToken).Scan(&teamID, &storedAuditID); err != nil {
		return fmt.Errorf("claim pending workflow: %w", err)
	}
	if auditTaskID != uuid.Nil && (storedAuditID == nil || *storedAuditID != auditTaskID) {
		return fmt.Errorf("workflow audit task mismatch")
	}
	if _, err := tx.ExecContext(ctx, `SELECT 1 FROM agent_teams WHERE id=$1 FOR UPDATE`, teamID); err != nil {
		return err
	}
	var terminalID *uuid.UUID
	for i := range tasks {
		tasks[i].TeamID = teamID
		tasks[i].WorkflowID = &workflowID
		tasks[i].WorkflowKind = store.TeamWorkflowTaskKindWork
		if err := insertPGWorkflowTask(ctx, tx, &tasks[i]); err != nil {
			return err
		}
		if tasks[i].WorkflowTerminal {
			id := tasks[i].ID
			terminalID = &id
		}
	}
	if storedAuditID != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=$1,result=$2,updated_at=$3 WHERE id=$4 AND tenant_id=$5 AND workflow_kind=$6`,
			store.TeamTaskStatusCompleted, "Workflow expansion accepted", time.Now(), *storedAuditID, tid, store.TeamWorkflowTaskKindAudit); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=$1,terminal_task_id=$2,expansion_token=NULL,expansion_lease_until=NULL,updated_at=$3 WHERE id=$4 AND tenant_id=$5 AND status=$6`,
		store.TeamWorkflowStatusRunning, terminalID, time.Now(), workflowID, tid, store.TeamWorkflowStatusPendingExpansion)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("pending workflow was already expanded")
	}
	return tx.Commit()
}

func (s *PGTeamStore) ClaimWorkflowTaskDispatch(ctx context.Context, taskID, teamID uuid.UUID, leaseUntil time.Time) (uuid.UUID, error) {
	token := uuid.New()
	// dispatch_count is a durable column (migration 000098); increment it
	// atomically as part of the claim so the circuit breaker has a single
	// source of truth. The owner partial-unique index
	// idx_team_tasks_active_owner is the real authority for owner exclusion —
	// a concurrent claim for the same owner surfaces as a 23505 which we map
	// to ErrWorkflowOwnerBusy so the loser does not increment the count.
	res, err := s.db.ExecContext(ctx, `UPDATE team_tasks t SET status=$1,dispatch_token=$2,dispatch_lease_until=$3,dispatch_count=t.dispatch_count+1,updated_at=$4
		WHERE t.id=$5 AND t.team_id=$6 AND t.tenant_id=$7 AND t.status=$8 AND t.workflow_kind=$9
		AND t.owner_agent_id IS NOT NULL AND COALESCE(array_length(t.blocked_by,1),0)=0
		AND EXISTS (SELECT 1 FROM team_workflows w WHERE w.id=t.workflow_id AND w.tenant_id=t.tenant_id AND w.status=$10)
		AND NOT EXISTS (SELECT 1 FROM team_tasks o WHERE o.tenant_id=t.tenant_id AND o.owner_agent_id=t.owner_agent_id
			AND o.workflow_kind=$9 AND o.status IN ($1,$11) AND o.id<>t.id)`,
		store.TeamTaskStatusDispatching, token, leaseUntil, time.Now(), taskID, teamID, tenantIDForInsert(ctx),
		store.TeamTaskStatusPending, store.TeamWorkflowTaskKindWork, store.TeamWorkflowStatusRunning, store.TeamTaskStatusInProgress)
	if err != nil {
		if isOwnerExclusionViolation(err) {
			return uuid.Nil, store.ErrWorkflowOwnerBusy
		}
		return uuid.Nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return uuid.Nil, fmt.Errorf("workflow task is not dispatchable")
	}
	return token, nil
}

// isOwnerExclusionViolation reports whether err is a Postgres unique-violation
// (23505) on the owner-exclusion partial index. The pre-check in the UPDATE
// predicate handles the common case; this catches the true concurrent race
// where two claims pass the check and one loses at the index.
func isOwnerExclusionViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return (strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key")) &&
		strings.Contains(msg, "idx_team_tasks_active_owner")
}

func (s *PGTeamStore) AcceptWorkflowTaskDispatch(ctx context.Context, taskID, teamID, dispatchToken uuid.UUID, lockExpiresAt time.Time) error {
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE team_tasks SET status=$1,locked_at=$2,lock_expires_at=$3,dispatch_lease_until=NULL,updated_at=$2
		WHERE id=$4 AND team_id=$5 AND tenant_id=$6 AND status=$7 AND workflow_kind=$8 AND dispatch_token=$9`,
		store.TeamTaskStatusInProgress, now, lockExpiresAt, taskID, teamID, tenantIDForInsert(ctx),
		store.TeamTaskStatusDispatching, store.TeamWorkflowTaskKindWork, dispatchToken)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("stale or duplicate workflow dispatch")
	}
	return nil
}

func (s *PGTeamStore) RequeueExpiredWorkflowDispatches(ctx context.Context, now time.Time) ([]store.TeamTaskData, error) {
	query := `UPDATE team_tasks SET status=$1,dispatch_token=NULL,dispatch_lease_until=NULL,updated_at=$2
		WHERE tenant_id=$3 AND status=$4 AND workflow_kind=$5 AND dispatch_lease_until < $2 RETURNING id`
	args := []any{store.TeamTaskStatusPending, now, tenantIDForInsert(ctx), store.TeamTaskStatusDispatching, store.TeamWorkflowTaskKindWork}
	if store.IsCrossTenant(ctx) {
		query = `UPDATE team_tasks SET status=$1,dispatch_token=NULL,dispatch_lease_until=NULL,updated_at=$2 WHERE status=$3 AND workflow_kind=$4 AND dispatch_lease_until < $2 RETURNING id`
		args = []any{store.TeamTaskStatusPending, now, store.TeamTaskStatusDispatching, store.TeamWorkflowTaskKindWork}
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
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
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return s.GetTasksByIDs(ctx, ids)
}

func (s *PGTeamStore) RecoverWorkflowRuns(ctx context.Context, force bool, now time.Time) ([]store.TeamTaskData, error) {
	if !store.IsCrossTenant(ctx) {
		return nil, fmt.Errorf("cross-tenant workflow recovery required")
	}
	condition := "AND lock_expires_at < $2"
	if force {
		condition = ""
	}
	rows, err := s.db.QueryContext(ctx, `UPDATE team_tasks SET status=$1,locked_at=NULL,lock_expires_at=NULL,dispatch_token=NULL,dispatch_lease_until=NULL,updated_at=$2 WHERE status=$3 AND workflow_kind=$4 `+condition+` RETURNING id`, store.TeamTaskStatusPending, now, store.TeamTaskStatusInProgress, store.TeamWorkflowTaskKindWork)
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

func (s *PGTeamStore) ListPendingAutoExpandWorkflows(ctx context.Context, now time.Time) ([]store.TeamWorkflowData, error) {
	if !store.IsCrossTenant(ctx) {
		return nil, fmt.Errorf("cross-tenant workflow recovery required")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE status=$1 AND auto_expand=TRUE AND (expansion_token IS NULL OR expansion_lease_until<$2) AND (next_expansion_at IS NULL OR next_expansion_at<=$2) ORDER BY created_at LIMIT 100`, store.TeamWorkflowStatusPendingExpansion, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var workflows []store.TeamWorkflowData
	for rows.Next() {
		workflow, err := scanPGWorkflow(rows)
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, *workflow)
	}
	return workflows, rows.Err()
}

func (s *PGTeamStore) ListWorkflowDispatchScopes(ctx context.Context) ([]store.TeamWorkflowDispatchScope, error) {
	if !store.IsCrossTenant(ctx) {
		return nil, fmt.Errorf("cross-tenant workflow recovery required")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT t.tenant_id,t.team_id,t.workflow_id FROM team_tasks t JOIN team_workflows w ON w.id=t.workflow_id AND w.tenant_id=t.tenant_id WHERE w.status=$1 AND t.workflow_kind=$2 AND t.status=$3 AND COALESCE(array_length(t.blocked_by,1),0)=0`, store.TeamWorkflowStatusRunning, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var scopes []store.TeamWorkflowDispatchScope
	for rows.Next() {
		var scope store.TeamWorkflowDispatchScope
		if err := rows.Scan(&scope.TenantID, &scope.TeamID, &scope.WorkflowID); err != nil {
			return nil, err
		}
		scopes = append(scopes, scope)
	}
	return scopes, rows.Err()
}

func (s *PGTeamStore) ListWorkflowsReadyToFinalize(ctx context.Context, now time.Time) ([]store.TeamWorkflowDispatchScope, error) {
	if !store.IsCrossTenant(ctx) {
		return nil, fmt.Errorf("cross-tenant workflow recovery required")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT w.tenant_id,w.team_id,w.id FROM team_workflows w WHERE
		(w.finalized_at IS NULL AND (w.finalize_token IS NULL OR w.finalize_lease_until<$1) AND
		 ((w.status=$2 AND NOT EXISTS (SELECT 1 FROM team_tasks t WHERE t.workflow_id=w.id AND t.tenant_id=w.tenant_id AND t.workflow_kind=$3 AND t.plan_revision=w.plan_revision AND t.status<>$4))
		  OR (w.status=$5 AND (w.failure_settle_deadline<=$1 OR NOT EXISTS (SELECT 1 FROM team_tasks t WHERE t.workflow_id=w.id AND t.tenant_id=w.tenant_id AND t.workflow_kind=$3 AND t.status IN ($6,$7))))
		  OR (w.status=$8 AND NOT EXISTS (SELECT 1 FROM team_tasks t WHERE t.workflow_id=w.id AND t.tenant_id=w.tenant_id AND t.workflow_kind=$3 AND t.status IN ($6,$7)))))
		OR (w.finalized_at IS NOT NULL AND w.delivered_at IS NULL AND w.delivery_status<>$9 AND (w.delivery_token IS NULL OR w.delivery_lease_until<$1) AND (w.next_delivery_at IS NULL OR w.next_delivery_at<=$1))`, now, store.TeamWorkflowStatusRunning, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusCompleted, store.TeamWorkflowStatusFailing, store.TeamTaskStatusDispatching, store.TeamTaskStatusInProgress, store.TeamWorkflowStatusCancelling, store.TeamWorkflowDeliveryDead)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var scopes []store.TeamWorkflowDispatchScope
	for rows.Next() {
		var scope store.TeamWorkflowDispatchScope
		if err := rows.Scan(&scope.TenantID, &scope.TeamID, &scope.WorkflowID); err != nil {
			return nil, err
		}
		scopes = append(scopes, scope)
	}
	return scopes, rows.Err()
}

func (s *PGTeamStore) SettleWorkflowTask(ctx context.Context, taskID, teamID uuid.UUID, result string, failed bool, failureSettleDeadline time.Time) (*store.TeamWorkflowSettlement, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	var workflowID uuid.UUID
	var taskStatus string
	if err := tx.QueryRowContext(ctx, `SELECT workflow_id,status FROM team_tasks WHERE id=$1 AND team_id=$2 AND tenant_id=$3 AND workflow_kind=$4 FOR UPDATE`, taskID, teamID, tid, store.TeamWorkflowTaskKindWork).Scan(&workflowID, &taskStatus); err != nil {
		return nil, err
	}
	now := time.Now()
	settleTask := taskStatus == store.TeamTaskStatusInProgress || (failed && (taskStatus == store.TeamTaskStatusPending || taskStatus == store.TeamTaskStatusBlocked || taskStatus == store.TeamTaskStatusDispatching))
	if settleTask {
		newStatus := store.TeamTaskStatusCompleted
		storedResult := result
		if failed {
			newStatus = store.TeamTaskStatusFailed
			storedResult = "FAILED: " + result
		}
		if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=$1,result=$2,locked_at=NULL,lock_expires_at=NULL,dispatch_token=NULL,dispatch_lease_until=NULL,updated_at=$3 WHERE id=$4 AND tenant_id=$5`, newStatus, storedResult, now, taskID, tid); err != nil {
			return nil, err
		}
		taskStatus = newStatus
		if !failed {
			if err := unblockDependentTasks(ctx, tx, taskID); err != nil {
				return nil, err
			}
		}
	}
	if failed || taskStatus == store.TeamTaskStatusFailed {
		if failureSettleDeadline.IsZero() {
			failureSettleDeadline = now.Add(2 * time.Minute)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=$1,failure_summary=CASE WHEN failure_summary='' THEN $2 ELSE failure_summary END,failure_settle_deadline=COALESCE(failure_settle_deadline,$3),updated_at=$4 WHERE id=$5 AND tenant_id=$6 AND status IN ($7,$1)`,
			store.TeamWorkflowStatusFailing, result, failureSettleDeadline, now, workflowID, tid, store.TeamWorkflowStatusRunning); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=$1,result=COALESCE(result,$2),updated_at=$3 WHERE workflow_id=$4 AND tenant_id=$5 AND workflow_kind=$6 AND status IN ($7,$8)`,
			store.TeamTaskStatusCancelled, "Cancelled because workflow failed", now, workflowID, tid, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusPending, store.TeamTaskStatusBlocked); err != nil {
			return nil, err
		}
	}
	var workflowStatus string
	var workflowRevision int
	var settleDeadline *time.Time
	if err := tx.QueryRowContext(ctx, `SELECT status,plan_revision,failure_settle_deadline FROM team_workflows WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, workflowID, tid).Scan(&workflowStatus, &workflowRevision, &settleDeadline); err != nil {
		return nil, err
	}
	ready := false
	if workflowStatus == store.TeamWorkflowStatusRunning {
		var unfinished int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_tasks WHERE workflow_id=$1 AND tenant_id=$2 AND workflow_kind=$3 AND plan_revision=$4 AND status<>$5`, workflowID, tid, store.TeamWorkflowTaskKindWork, workflowRevision, store.TeamTaskStatusCompleted).Scan(&unfinished); err != nil {
			return nil, err
		}
		ready = unfinished == 0
	} else if workflowStatus == store.TeamWorkflowStatusFailing {
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_tasks WHERE workflow_id=$1 AND tenant_id=$2 AND workflow_kind=$3 AND status IN ($4,$5)`, workflowID, tid, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusDispatching, store.TeamTaskStatusInProgress).Scan(&active); err != nil {
			return nil, err
		}
		ready = active == 0 || (settleDeadline != nil && !settleDeadline.After(now))
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &store.TeamWorkflowSettlement{WorkflowID: workflowID, WorkflowStatus: workflowStatus, ReadyToFinalize: ready}, nil
}

func (s *PGTeamStore) ClaimWorkflowFinalization(ctx context.Context, workflowID uuid.UUID, leaseUntil time.Time) (*store.TeamWorkflowData, uuid.UUID, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, uuid.Nil, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	now := time.Now()
	token := uuid.New()
	res, err := tx.ExecContext(ctx, `UPDATE team_workflows w SET finalize_token=$1,finalize_lease_until=$2,finalize_claimed_at=$3,updated_at=$3
		WHERE w.id=$4 AND w.tenant_id=$5 AND w.finalized_at IS NULL AND (w.finalize_token IS NULL OR w.finalize_lease_until<$3)
		AND ((w.status=$6 AND NOT EXISTS (SELECT 1 FROM team_tasks t WHERE t.workflow_id=w.id AND t.tenant_id=w.tenant_id AND t.workflow_kind=$7 AND t.plan_revision=w.plan_revision AND t.status<>$8))
		 OR (w.status=$9 AND (w.failure_settle_deadline<=$3 OR NOT EXISTS (SELECT 1 FROM team_tasks t WHERE t.workflow_id=w.id AND t.tenant_id=w.tenant_id AND t.workflow_kind=$7 AND t.status IN ($10,$11))))
		 OR (w.status=$12 AND NOT EXISTS (SELECT 1 FROM team_tasks t WHERE t.workflow_id=w.id AND t.tenant_id=w.tenant_id AND t.workflow_kind=$7 AND t.status IN ($10,$11))))`,
		token, leaseUntil, now, workflowID, tid, store.TeamWorkflowStatusRunning, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusCompleted,
		store.TeamWorkflowStatusFailing, store.TeamTaskStatusDispatching, store.TeamTaskStatusInProgress, store.TeamWorkflowStatusCancelling)
	if err != nil {
		return nil, uuid.Nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, uuid.Nil, fmt.Errorf("workflow is not ready for finalization")
	}
	w, err := scanPGWorkflow(tx.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE id=$1 AND tenant_id=$2`, workflowID, tid))
	if err != nil {
		return nil, uuid.Nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, uuid.Nil, err
	}
	return w, token, nil
}

func (s *PGTeamStore) CompleteWorkflowFinalization(ctx context.Context, workflowID, finalizeToken uuid.UUID, status, resultSummary string) error {
	if status != store.TeamWorkflowStatusCompleted && status != store.TeamWorkflowStatusFailed && status != store.TeamWorkflowStatusCancelled {
		return fmt.Errorf("invalid final workflow status")
	}
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE team_workflows SET status=$1,result_summary=$2,finalized_at=$3,finalize_token=NULL,finalize_lease_until=NULL,delivery_status=$4,delivery_token=NULL,delivery_lease_until=NULL,updated_at=$3 WHERE id=$5 AND tenant_id=$6 AND finalize_token=$7 AND finalized_at IS NULL`, status, resultSummary, now, store.TeamWorkflowDeliveryPending, workflowID, tenantIDForInsert(ctx), finalizeToken)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("stale workflow finalize token")
	}
	return nil
}

func (s *PGTeamStore) ClaimWorkflowDelivery(ctx context.Context, workflowID uuid.UUID, leaseUntil time.Time) (*store.TeamWorkflowData, uuid.UUID, error) {
	token, now := uuid.New(), time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE team_workflows SET delivery_status=$1,delivery_token=$2,delivery_lease_until=$3,updated_at=$4 WHERE id=$5 AND tenant_id=$6 AND finalized_at IS NOT NULL AND delivered_at IS NULL AND delivery_status=$7 AND (next_delivery_at IS NULL OR next_delivery_at<=$4) AND (delivery_token IS NULL OR delivery_lease_until<$4)`, store.TeamWorkflowDeliveryEnqueuing, token, leaseUntil, now, workflowID, tenantIDForInsert(ctx), store.TeamWorkflowDeliveryPending)
	if err != nil {
		return nil, uuid.Nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, uuid.Nil, fmt.Errorf("workflow delivery is already claimed")
	}
	w, err := s.GetWorkflow(ctx, workflowID)
	return w, token, err
}

func (s *PGTeamStore) CompleteWorkflowDelivery(ctx context.Context, workflowID, deliveryToken uuid.UUID) error {
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE team_workflows SET delivery_status=$1,delivered_at=$2,delivery_token=NULL,delivery_lease_until=NULL,updated_at=$2 WHERE id=$3 AND tenant_id=$4 AND delivery_token=$5 AND delivered_at IS NULL`, store.TeamWorkflowDeliveryDelivered, now, workflowID, tenantIDForInsert(ctx), deliveryToken)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("stale workflow delivery token")
	}
	return nil
}

func (s *PGTeamStore) SearchWorkflows(ctx context.Context, teamID uuid.UUID, query string, limit int) ([]store.TeamWorkflowData, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE tenant_id=$1 AND team_id=$2 AND (canonical_plan::text ILIKE $3 OR plan_hash=$4) ORDER BY created_at DESC LIMIT $5`, tenantIDForInsert(ctx), teamID, "%"+strings.TrimSpace(query)+"%", strings.TrimSpace(query), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var workflows []store.TeamWorkflowData
	for rows.Next() {
		w, scanErr := scanPGWorkflow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		workflows = append(workflows, *w)
	}
	return workflows, rows.Err()
}

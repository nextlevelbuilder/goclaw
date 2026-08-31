//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

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

func (s *SQLiteTeamStore) CreateWorkflowWithTasks(ctx context.Context, workflow *store.TeamWorkflowData, tasks []store.TeamTaskData) error {
	if len(tasks) == 0 {
		return fmt.Errorf("workflow tasks are required")
	}
	return s.createWorkflow(ctx, workflow, tasks, nil)
}

func (s *SQLiteTeamStore) CreatePendingWorkflowRequest(ctx context.Context, workflow *store.TeamWorkflowData, auditTask *store.TeamTaskData) error {
	if auditTask == nil {
		return fmt.Errorf("workflow audit task is required")
	}
	if auditTask.OwnerAgentID == nil || *auditTask.OwnerAgentID != workflow.CoordinatorAgentID {
		return fmt.Errorf("workflow audit owner must match canonical coordinator")
	}
	return s.createWorkflow(ctx, workflow, nil, auditTask)
}

func (s *SQLiteTeamStore) createWorkflow(ctx context.Context, workflow *store.TeamWorkflowData, tasks []store.TeamTaskData, auditTask *store.TeamTaskData) error {
	if err := prepareSQLiteWorkflow(ctx, workflow); err != nil {
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
	if err := insertSQLiteWorkflow(ctx, tx, workflow); err != nil {
		return err
	}
	if auditTask != nil {
		auditTask.WorkflowID = &workflow.ID
		auditTask.WorkflowKind = store.TeamWorkflowTaskKindAudit
		auditTask.WorkflowStepID = ""
		auditTask.WorkflowTerminal = false
		if err := insertSQLiteWorkflowTask(ctx, tx, auditTask); err != nil {
			return err
		}
		workflow.AuditTaskID = &auditTask.ID
	}
	for i := range tasks {
		tasks[i].WorkflowID = &workflow.ID
		tasks[i].WorkflowKind = store.TeamWorkflowTaskKindWork
		if err := insertSQLiteWorkflowTask(ctx, tx, &tasks[i]); err != nil {
			return err
		}
		if tasks[i].WorkflowTerminal {
			id := tasks[i].ID
			workflow.TerminalTaskID = &id
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET audit_task_id=?,terminal_task_id=?,updated_at=? WHERE id=? AND tenant_id=?`,
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

func prepareSQLiteWorkflow(ctx context.Context, workflow *store.TeamWorkflowData) error {
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
	workflow.CreatedAt, workflow.UpdatedAt = now, now
	return nil
}

func insertSQLiteWorkflow(ctx context.Context, tx *sql.Tx, w *store.TeamWorkflowData) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO team_workflows (
		id,team_id,tenant_id,status,canonical_plan,schema_version,plan_hash,
		coordinator_agent_id,coordinator_agent_key,origin_agent_id,origin_agent_key,origin_run_id,
		origin_session_key,origin_channel,origin_chat_id,origin_peer_kind,origin_local_key,origin_user_id,origin_sender_id,origin_role,origin_routing,
		auto_expand,failure_summary,result_summary,classification_audit_id,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		w.ID, w.TeamID, w.TenantID, w.Status, []byte(w.CanonicalPlan), w.SchemaVersion, w.PlanHash,
		w.CoordinatorAgentID, w.CoordinatorAgentKey, w.OriginAgentID, w.OriginAgentKey, w.OriginRunID,
		w.OriginSessionKey, w.OriginChannel, w.OriginChatID, w.OriginPeerKind, w.OriginLocalKey, w.OriginUserID, w.OriginSenderID, w.OriginRole, []byte(w.OriginRouting),
		w.AutoExpand, w.FailureSummary, w.ResultSummary, w.ClassificationAuditID, w.CreatedAt, w.UpdatedAt)
	return err
}

func insertSQLiteWorkflowTask(ctx context.Context, tx *sql.Tx, task *store.TeamTaskData) error {
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
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(task_number),0)+1 FROM team_tasks WHERE team_id=? AND COALESCE(chat_id,'')=?`, task.TeamID, task.ChatID).Scan(&taskNumber); err != nil {
		return err
	}
	task.TaskNumber = taskNumber
	hex := strings.ReplaceAll(task.ID.String(), "-", "")
	task.Identifier = fmt.Sprintf("T-%03d-%s", taskNumber, hex[len(hex)-4:])
	// Promote a legacy metadata dispatch_count seed into the durable column
	// (60→61) before insert; the column is what the atomic claim increments.
	task.SeedDispatchCountFromMetadata()
	meta := []byte(`{}`)
	if len(task.Metadata) > 0 {
		meta, _ = json.Marshal(task.Metadata)
	}
	blocked := jsonStringArray(uuidSliceToStrings(task.BlockedBy))
	now := time.Now()
	task.CreatedAt, task.UpdatedAt = now, now
	_, err := tx.ExecContext(ctx, `INSERT INTO team_tasks (
		id,team_id,tenant_id,subject,description,status,owner_agent_id,blocked_by,priority,result,user_id,channel,
		task_type,task_number,identifier,created_by_agent_id,parent_id,chat_id,metadata,
		workflow_id,workflow_step_id,workflow_kind,workflow_terminal,plan_revision,dispatch_count,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		task.ID, task.TeamID, tenantIDForInsert(ctx), task.Subject, task.Description, task.Status,
		task.OwnerAgentID, blocked, task.Priority, task.Result, nilStr(task.UserID), nilStr(task.Channel),
		task.TaskType, task.TaskNumber, task.Identifier, task.CreatedByAgentID, task.ParentID, nilStr(task.ChatID), meta,
		task.WorkflowID, nilStr(task.WorkflowStepID), task.WorkflowKind, task.WorkflowTerminal,
		store.PlanRevisionOrDefault(task.PlanRevision), task.DispatchCount, now, now)
	return err
}

func (s *SQLiteTeamStore) GetWorkflow(ctx context.Context, workflowID uuid.UUID) (*store.TeamWorkflowData, error) {
	return scanSQLiteWorkflow(s.db.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE id=? AND tenant_id=?`, workflowID, tenantIDForInsert(ctx)))
}

func (s *SQLiteTeamStore) FindWorkflowByCreationKey(ctx context.Context, teamID uuid.UUID, originRunID, planHash string) (*store.TeamWorkflowData, error) {
	return scanSQLiteWorkflow(s.db.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE tenant_id=? AND team_id=? AND origin_run_id=? AND plan_hash=?`, tenantIDForInsert(ctx), teamID, originRunID, planHash))
}

type sqliteWorkflowRowScanner interface{ Scan(...any) error }

func scanSQLiteWorkflow(row sqliteWorkflowRowScanner) (*store.TeamWorkflowData, error) {
	var w store.TeamWorkflowData
	var plan, originRouting []byte
	var expansionLease, finalizeLease, finalizeClaimed, finalized, failureDeadline, deliveryLease, delivered nullSqliteTime
	var nextExpansionAt, nextDeliveryAt, cancelledAt nullSqliteTime
	createdAt, updatedAt := scanTimePair()
	if err := row.Scan(&w.ID, &w.TeamID, &w.TenantID, &w.Status, &plan, &w.SchemaVersion, &w.PlanHash,
		&w.CoordinatorAgentID, &w.CoordinatorAgentKey, &w.OriginAgentID, &w.OriginAgentKey, &w.OriginRunID,
		&w.OriginSessionKey, &w.OriginChannel, &w.OriginChatID, &w.OriginPeerKind, &w.OriginLocalKey, &w.OriginUserID, &w.OriginSenderID, &w.OriginRole, &originRouting,
		&w.AutoExpand, &w.AuditTaskID, &w.TerminalTaskID, &w.ExpansionToken, &expansionLease,
		&w.FinalizeToken, &finalizeLease, &finalizeClaimed, &finalized, &failureDeadline,
		&w.FailureSummary, &w.ResultSummary, &w.DeliveryStatus, &w.DeliveryToken, &deliveryLease, &delivered, createdAt, updatedAt,
		&w.PlanRevision, &w.ExpansionAttemptCount, &nextExpansionAt, &w.LastExpansionError,
		&w.DeliveryAttemptCount, &nextDeliveryAt, &w.LastDeliveryError, &w.CancelReason, &cancelledAt, &w.ClassificationAuditID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrTaskNotFound
		}
		return nil, err
	}
	w.CanonicalPlan = plan
	w.OriginRouting = originRouting
	w.CreatedAt, w.UpdatedAt = createdAt.Time, updatedAt.Time
	if expansionLease.Valid {
		w.ExpansionLeaseUntil = &expansionLease.Time
	}
	if finalizeLease.Valid {
		w.FinalizeLeaseUntil = &finalizeLease.Time
	}
	if finalizeClaimed.Valid {
		w.FinalizeClaimedAt = &finalizeClaimed.Time
	}
	if finalized.Valid {
		w.FinalizedAt = &finalized.Time
	}
	if failureDeadline.Valid {
		w.FailureSettleDeadline = &failureDeadline.Time
	}
	if deliveryLease.Valid {
		w.DeliveryLeaseUntil = &deliveryLease.Time
	}
	if delivered.Valid {
		w.DeliveredAt = &delivered.Time
	}
	if nextExpansionAt.Valid {
		w.NextExpansionAt = &nextExpansionAt.Time
	}
	if nextDeliveryAt.Valid {
		w.NextDeliveryAt = &nextDeliveryAt.Time
	}
	if cancelledAt.Valid {
		w.CancelledAt = &cancelledAt.Time
	}
	return &w, nil
}

func (s *SQLiteTeamStore) ListWorkflowTasks(ctx context.Context, workflowID uuid.UUID) ([]store.TeamTaskData, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+taskSelectCols+` `+taskJoinClause+` JOIN team_workflows w ON w.id=t.workflow_id AND w.tenant_id=t.tenant_id AND w.team_id=t.team_id WHERE t.workflow_id=? AND t.tenant_id=? ORDER BY t.task_number`, workflowID, tenantIDForInsert(ctx))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

func (s *SQLiteTeamStore) ClaimPendingWorkflowExpansion(ctx context.Context, workflowID, coordinatorID uuid.UUID, leaseUntil time.Time) (uuid.UUID, error) {
	token, now := uuid.New(), time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE team_workflows SET expansion_token=?,expansion_lease_until=?,updated_at=? WHERE id=? AND tenant_id=? AND coordinator_agent_id=? AND status=? AND (expansion_token IS NULL OR expansion_lease_until<?)`, token, leaseUntil, now, workflowID, tenantIDForInsert(ctx), coordinatorID, store.TeamWorkflowStatusPendingExpansion, now)
	if err != nil {
		return uuid.Nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return uuid.Nil, fmt.Errorf("pending workflow expansion is already claimed")
	}
	return token, nil
}

func (s *SQLiteTeamStore) ExpandPendingWorkflow(ctx context.Context, workflowID, coordinatorID, expansionToken uuid.UUID, tasks []store.TeamTaskData) error {
	return s.expandPendingWorkflow(ctx, workflowID, uuid.Nil, coordinatorID, expansionToken, tasks)
}

func (s *SQLiteTeamStore) ApprovePendingWorkflowRequest(ctx context.Context, workflowID, auditTaskID uuid.UUID, actor store.WorkflowApprovalActor, tasks []store.TeamTaskData) error {
	workflow, err := s.GetWorkflow(ctx, workflowID)
	if err != nil {
		return err
	}
	if err := authorizeSQLiteWorkflowApproval(actor, workflow.CoordinatorAgentID); err != nil {
		return err
	}
	token, err := s.ClaimPendingWorkflowExpansion(ctx, workflowID, workflow.CoordinatorAgentID, time.Now().Add(2*time.Minute))
	if err != nil {
		return err
	}
	return s.expandPendingWorkflow(ctx, workflowID, auditTaskID, workflow.CoordinatorAgentID, token, tasks)
}

func authorizeSQLiteWorkflowApproval(actor store.WorkflowApprovalActor, coordinatorID uuid.UUID) error {
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

func (s *SQLiteTeamStore) expandPendingWorkflow(ctx context.Context, workflowID, auditTaskID, coordinatorID, expansionToken uuid.UUID, tasks []store.TeamTaskData) error {
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
	if err := tx.QueryRowContext(ctx, `SELECT team_id,audit_task_id FROM team_workflows WHERE id=? AND tenant_id=? AND status=? AND coordinator_agent_id=? AND expansion_token=?`, workflowID, tid, store.TeamWorkflowStatusPendingExpansion, coordinatorID, expansionToken).Scan(&teamID, &storedAuditID); err != nil {
		return fmt.Errorf("claim pending workflow: %w", err)
	}
	if auditTaskID != uuid.Nil && (storedAuditID == nil || *storedAuditID != auditTaskID) {
		return fmt.Errorf("workflow audit task mismatch")
	}
	var terminalID *uuid.UUID
	for i := range tasks {
		tasks[i].TeamID = teamID
		tasks[i].WorkflowID = &workflowID
		tasks[i].WorkflowKind = store.TeamWorkflowTaskKindWork
		if err := insertSQLiteWorkflowTask(ctx, tx, &tasks[i]); err != nil {
			return err
		}
		if tasks[i].WorkflowTerminal {
			id := tasks[i].ID
			terminalID = &id
		}
	}
	if storedAuditID != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,result=?,updated_at=? WHERE id=? AND tenant_id=? AND workflow_kind=?`, store.TeamTaskStatusCompleted, "Workflow expansion accepted", time.Now(), *storedAuditID, tid, store.TeamWorkflowTaskKindAudit); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=?,terminal_task_id=?,expansion_token=NULL,expansion_lease_until=NULL,updated_at=? WHERE id=? AND tenant_id=? AND status=?`, store.TeamWorkflowStatusRunning, terminalID, time.Now(), workflowID, tid, store.TeamWorkflowStatusPendingExpansion)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("pending workflow was already expanded")
	}
	return tx.Commit()
}

func (s *SQLiteTeamStore) ClaimWorkflowTaskDispatch(ctx context.Context, taskID, teamID uuid.UUID, leaseUntil time.Time) (uuid.UUID, error) {
	token := uuid.New()
	// dispatch_count is a durable column (migration 60→61); increment it in
	// the claim itself. The owner partial-unique index idx_team_tasks_active_owner
	// is the authority for owner exclusion; the NOT EXISTS pre-check keeps the
	// common case from even attempting the write, and a genuine race surfaces as
	// a UNIQUE constraint error mapped to ErrWorkflowOwnerBusy.
	res, err := s.db.ExecContext(ctx, `UPDATE team_tasks SET status=?,dispatch_token=?,dispatch_lease_until=?,dispatch_count=dispatch_count+1,updated_at=?
		WHERE id=? AND team_id=? AND tenant_id=? AND status=? AND workflow_kind=? AND owner_agent_id IS NOT NULL
		AND json_array_length(COALESCE(blocked_by,'[]'))=0
		AND EXISTS (SELECT 1 FROM team_workflows w WHERE w.id=team_tasks.workflow_id AND w.tenant_id=team_tasks.tenant_id AND w.status=?)
		AND NOT EXISTS (SELECT 1 FROM team_tasks o WHERE o.tenant_id=team_tasks.tenant_id AND o.owner_agent_id=team_tasks.owner_agent_id
			AND o.workflow_kind=? AND o.status IN (?,?) AND o.id<>team_tasks.id)`,
		store.TeamTaskStatusDispatching, token, leaseUntil, time.Now(), taskID, teamID, tenantIDForInsert(ctx),
		store.TeamTaskStatusPending, store.TeamWorkflowTaskKindWork, store.TeamWorkflowStatusRunning,
		store.TeamWorkflowTaskKindWork, store.TeamTaskStatusDispatching, store.TeamTaskStatusInProgress)
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

// isOwnerExclusionViolation reports whether err is a SQLite UNIQUE-constraint
// failure on the owner-exclusion partial index.
func isOwnerExclusionViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") &&
		strings.Contains(msg, "idx_team_tasks_active_owner")
}

func (s *SQLiteTeamStore) AcceptWorkflowTaskDispatch(ctx context.Context, taskID, teamID, dispatchToken uuid.UUID, lockExpiresAt time.Time) error {
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE team_tasks SET status=?,locked_at=?,lock_expires_at=?,dispatch_lease_until=NULL,updated_at=?
		WHERE id=? AND team_id=? AND tenant_id=? AND status=? AND workflow_kind=? AND dispatch_token=?`,
		store.TeamTaskStatusInProgress, now, lockExpiresAt, now, taskID, teamID, tenantIDForInsert(ctx),
		store.TeamTaskStatusDispatching, store.TeamWorkflowTaskKindWork, dispatchToken)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("stale or duplicate workflow dispatch")
	}
	return nil
}

func (s *SQLiteTeamStore) RequeueExpiredWorkflowDispatches(ctx context.Context, now time.Time) ([]store.TeamTaskData, error) {
	if store.IsCrossTenant(ctx) {
		rows, err := s.db.QueryContext(ctx, `SELECT id FROM team_tasks WHERE status=? AND workflow_kind=? AND dispatch_lease_until < ?`, store.TeamTaskStatusDispatching, store.TeamWorkflowTaskKindWork, now)
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
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
		args := []any{store.TeamTaskStatusPending, now, store.TeamTaskStatusDispatching, store.TeamWorkflowTaskKindWork}
		for _, id := range ids {
			args = append(args, id)
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE team_tasks SET status=?,dispatch_token=NULL,dispatch_lease_until=NULL,updated_at=? WHERE status=? AND workflow_kind=? AND id IN (`+placeholders+`)`, args...); err != nil {
			return nil, err
		}
		return s.GetTasksByIDs(ctx, ids)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM team_tasks WHERE tenant_id=? AND status=? AND workflow_kind=? AND dispatch_lease_until < ?`,
		tenantIDForInsert(ctx), store.TeamTaskStatusDispatching, store.TeamWorkflowTaskKindWork, now)
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
		_ = tx.Commit()
		return nil, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := []any{store.TeamTaskStatusPending, now, tenantIDForInsert(ctx), store.TeamTaskStatusDispatching, store.TeamWorkflowTaskKindWork}
	for _, id := range ids {
		args = append(args, id)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,dispatch_token=NULL,dispatch_lease_until=NULL,updated_at=? WHERE tenant_id=? AND status=? AND workflow_kind=? AND id IN (`+placeholders+`)`, args...); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetTasksByIDs(ctx, ids)
}

func (s *SQLiteTeamStore) RecoverWorkflowRuns(ctx context.Context, force bool, now time.Time) ([]store.TeamTaskData, error) {
	if !store.IsCrossTenant(ctx) {
		return nil, fmt.Errorf("cross-tenant workflow recovery required")
	}
	query := `SELECT id FROM team_tasks WHERE status=? AND workflow_kind=? AND lock_expires_at < ?`
	args := []any{store.TeamTaskStatusInProgress, store.TeamWorkflowTaskKindWork, now}
	if force {
		query = `SELECT id FROM team_tasks WHERE status=? AND workflow_kind=?`
		args = []any{store.TeamTaskStatusInProgress, store.TeamWorkflowTaskKindWork}
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
	rows.Close()
	if len(ids) == 0 {
		return nil, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	updateArgs := []any{store.TeamTaskStatusPending, now, store.TeamTaskStatusInProgress, store.TeamWorkflowTaskKindWork}
	for _, id := range ids {
		updateArgs = append(updateArgs, id)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE team_tasks SET status=?,locked_at=NULL,lock_expires_at=NULL,dispatch_token=NULL,dispatch_lease_until=NULL,updated_at=? WHERE status=? AND workflow_kind=? AND id IN (`+ph+`)`, updateArgs...); err != nil {
		return nil, err
	}
	return s.GetTasksByIDs(ctx, ids)
}

func (s *SQLiteTeamStore) ListPendingAutoExpandWorkflows(ctx context.Context, now time.Time) ([]store.TeamWorkflowData, error) {
	if !store.IsCrossTenant(ctx) {
		return nil, fmt.Errorf("cross-tenant workflow recovery required")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE status=? AND auto_expand=1 AND (expansion_token IS NULL OR expansion_lease_until<?) AND (next_expansion_at IS NULL OR next_expansion_at<=?) ORDER BY created_at LIMIT 100`, store.TeamWorkflowStatusPendingExpansion, now, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var workflows []store.TeamWorkflowData
	for rows.Next() {
		workflow, err := scanSQLiteWorkflow(rows)
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, *workflow)
	}
	return workflows, rows.Err()
}

func (s *SQLiteTeamStore) ListWorkflowDispatchScopes(ctx context.Context) ([]store.TeamWorkflowDispatchScope, error) {
	if !store.IsCrossTenant(ctx) {
		return nil, fmt.Errorf("cross-tenant workflow recovery required")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT t.tenant_id,t.team_id,t.workflow_id FROM team_tasks t JOIN team_workflows w ON w.id=t.workflow_id AND w.tenant_id=t.tenant_id WHERE w.status=? AND t.workflow_kind=? AND t.status=? AND json_array_length(COALESCE(t.blocked_by,'[]'))=0`, store.TeamWorkflowStatusRunning, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusPending)
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

func (s *SQLiteTeamStore) ListWorkflowsReadyToFinalize(ctx context.Context, now time.Time) ([]store.TeamWorkflowDispatchScope, error) {
	if !store.IsCrossTenant(ctx) {
		return nil, fmt.Errorf("cross-tenant workflow recovery required")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT w.tenant_id,w.team_id,w.id FROM team_workflows w WHERE
		(w.finalized_at IS NULL AND (w.finalize_token IS NULL OR w.finalize_lease_until<?) AND
		 ((w.status=? AND NOT EXISTS (SELECT 1 FROM team_tasks t WHERE t.workflow_id=w.id AND t.tenant_id=w.tenant_id AND t.workflow_kind=? AND t.plan_revision=w.plan_revision AND t.status<>?))
		  OR (w.status=? AND (w.failure_settle_deadline<=? OR NOT EXISTS (SELECT 1 FROM team_tasks t WHERE t.workflow_id=w.id AND t.tenant_id=w.tenant_id AND t.workflow_kind=? AND t.status IN (?,?))))
		  OR (w.status=? AND NOT EXISTS (SELECT 1 FROM team_tasks t WHERE t.workflow_id=w.id AND t.tenant_id=w.tenant_id AND t.workflow_kind=? AND t.status IN (?,?)))))
		OR (w.finalized_at IS NOT NULL AND w.delivered_at IS NULL AND w.delivery_status<>? AND (w.delivery_token IS NULL OR w.delivery_lease_until<?) AND (w.next_delivery_at IS NULL OR w.next_delivery_at<=?))`, now, store.TeamWorkflowStatusRunning, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusCompleted, store.TeamWorkflowStatusFailing, now, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusDispatching, store.TeamTaskStatusInProgress, store.TeamWorkflowStatusCancelling, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusDispatching, store.TeamTaskStatusInProgress, store.TeamWorkflowDeliveryDead, now, now)
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

func (s *SQLiteTeamStore) SettleWorkflowTask(ctx context.Context, taskID, teamID uuid.UUID, result string, failed bool, failureSettleDeadline time.Time) (*store.TeamWorkflowSettlement, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tid := tenantIDForInsert(ctx)
	var workflowID uuid.UUID
	var taskStatus string
	if err := tx.QueryRowContext(ctx, `SELECT workflow_id,status FROM team_tasks WHERE id=? AND team_id=? AND tenant_id=? AND workflow_kind=?`, taskID, teamID, tid, store.TeamWorkflowTaskKindWork).Scan(&workflowID, &taskStatus); err != nil {
		return nil, err
	}
	now := time.Now()
	settleTask := taskStatus == store.TeamTaskStatusInProgress || (failed && (taskStatus == store.TeamTaskStatusPending || taskStatus == store.TeamTaskStatusBlocked || taskStatus == store.TeamTaskStatusDispatching))
	if settleTask {
		newStatus, storedResult := store.TeamTaskStatusCompleted, result
		if failed {
			newStatus, storedResult = store.TeamTaskStatusFailed, "FAILED: "+result
		}
		if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,result=?,locked_at=NULL,lock_expires_at=NULL,dispatch_token=NULL,dispatch_lease_until=NULL,updated_at=? WHERE id=? AND tenant_id=?`, newStatus, storedResult, now, taskID, tid); err != nil {
			return nil, err
		}
		taskStatus = newStatus
		if !failed {
			if err := unblockDependentTasksSQLite(ctx, tx, taskID); err != nil {
				return nil, err
			}
		}
	}
	if failed || taskStatus == store.TeamTaskStatusFailed {
		if failureSettleDeadline.IsZero() {
			failureSettleDeadline = now.Add(2 * time.Minute)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE team_workflows SET status=?,failure_summary=CASE WHEN failure_summary='' THEN ? ELSE failure_summary END,failure_settle_deadline=COALESCE(failure_settle_deadline,?),updated_at=? WHERE id=? AND tenant_id=? AND status IN (?,?)`, store.TeamWorkflowStatusFailing, result, failureSettleDeadline, now, workflowID, tid, store.TeamWorkflowStatusRunning, store.TeamWorkflowStatusFailing); err != nil {
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE team_tasks SET status=?,result=COALESCE(result,?),updated_at=? WHERE workflow_id=? AND tenant_id=? AND workflow_kind=? AND status IN (?,?)`, store.TeamTaskStatusCancelled, "Cancelled because workflow failed", now, workflowID, tid, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusPending, store.TeamTaskStatusBlocked); err != nil {
			return nil, err
		}
	}
	var workflowStatus string
	var settleDeadline nullSqliteTime
	var planRevision int
	if err := tx.QueryRowContext(ctx, `SELECT status,failure_settle_deadline,plan_revision FROM team_workflows WHERE id=? AND tenant_id=?`, workflowID, tid).Scan(&workflowStatus, &settleDeadline, &planRevision); err != nil {
		return nil, err
	}
	ready := false
	if workflowStatus == store.TeamWorkflowStatusRunning {
		var unfinished int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_tasks WHERE workflow_id=? AND tenant_id=? AND workflow_kind=? AND plan_revision=? AND status<>?`, workflowID, tid, store.TeamWorkflowTaskKindWork, planRevision, store.TeamTaskStatusCompleted).Scan(&unfinished); err != nil {
			return nil, err
		}
		ready = unfinished == 0
	} else if workflowStatus == store.TeamWorkflowStatusFailing {
		var active int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_tasks WHERE workflow_id=? AND tenant_id=? AND workflow_kind=? AND status IN (?,?)`, workflowID, tid, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusDispatching, store.TeamTaskStatusInProgress).Scan(&active); err != nil {
			return nil, err
		}
		ready = active == 0 || (settleDeadline.Valid && !settleDeadline.Time.After(now))
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &store.TeamWorkflowSettlement{WorkflowID: workflowID, WorkflowStatus: workflowStatus, ReadyToFinalize: ready}, nil
}

func (s *SQLiteTeamStore) ClaimWorkflowFinalization(ctx context.Context, workflowID uuid.UUID, leaseUntil time.Time) (*store.TeamWorkflowData, uuid.UUID, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, uuid.Nil, err
	}
	defer tx.Rollback()
	tid, now, token := tenantIDForInsert(ctx), time.Now(), uuid.New()
	res, err := tx.ExecContext(ctx, `UPDATE team_workflows SET finalize_token=?,finalize_lease_until=?,finalize_claimed_at=?,updated_at=?
		WHERE id=? AND tenant_id=? AND finalized_at IS NULL AND (finalize_token IS NULL OR finalize_lease_until<?)
		AND ((status=? AND NOT EXISTS (SELECT 1 FROM team_tasks t WHERE t.workflow_id=team_workflows.id AND t.tenant_id=team_workflows.tenant_id AND t.workflow_kind=? AND t.plan_revision=team_workflows.plan_revision AND t.status<>?))
		 OR (status=? AND (failure_settle_deadline<=? OR NOT EXISTS (SELECT 1 FROM team_tasks t WHERE t.workflow_id=team_workflows.id AND t.tenant_id=team_workflows.tenant_id AND t.workflow_kind=? AND t.status IN (?,?))))
		 OR (status=? AND NOT EXISTS (SELECT 1 FROM team_tasks t WHERE t.workflow_id=team_workflows.id AND t.tenant_id=team_workflows.tenant_id AND t.workflow_kind=? AND t.status IN (?,?))))`,
		token, leaseUntil, now, now, workflowID, tid, now, store.TeamWorkflowStatusRunning, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusCompleted,
		store.TeamWorkflowStatusFailing, now, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusDispatching, store.TeamTaskStatusInProgress,
		store.TeamWorkflowStatusCancelling, store.TeamWorkflowTaskKindWork, store.TeamTaskStatusDispatching, store.TeamTaskStatusInProgress)
	if err != nil {
		return nil, uuid.Nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, uuid.Nil, fmt.Errorf("workflow is not ready for finalization")
	}
	w, err := scanSQLiteWorkflow(tx.QueryRowContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE id=? AND tenant_id=?`, workflowID, tid))
	if err != nil {
		return nil, uuid.Nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, uuid.Nil, err
	}
	return w, token, nil
}

func (s *SQLiteTeamStore) CompleteWorkflowFinalization(ctx context.Context, workflowID, finalizeToken uuid.UUID, status, resultSummary string) error {
	if status != store.TeamWorkflowStatusCompleted && status != store.TeamWorkflowStatusFailed && status != store.TeamWorkflowStatusCancelled {
		return fmt.Errorf("invalid final workflow status")
	}
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE team_workflows SET status=?,result_summary=?,finalized_at=?,finalize_token=NULL,finalize_lease_until=NULL,delivery_status=?,delivery_token=NULL,delivery_lease_until=NULL,updated_at=? WHERE id=? AND tenant_id=? AND finalize_token=? AND finalized_at IS NULL`, status, resultSummary, now, store.TeamWorkflowDeliveryPending, now, workflowID, tenantIDForInsert(ctx), finalizeToken)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("stale workflow finalize token")
	}
	return nil
}

func (s *SQLiteTeamStore) ClaimWorkflowDelivery(ctx context.Context, workflowID uuid.UUID, leaseUntil time.Time) (*store.TeamWorkflowData, uuid.UUID, error) {
	token, now := uuid.New(), time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE team_workflows SET delivery_status=?,delivery_token=?,delivery_lease_until=?,updated_at=? WHERE id=? AND tenant_id=? AND finalized_at IS NOT NULL AND delivered_at IS NULL AND delivery_status=? AND (next_delivery_at IS NULL OR next_delivery_at<=?) AND (delivery_token IS NULL OR delivery_lease_until<?)`, store.TeamWorkflowDeliveryEnqueuing, token, leaseUntil, now, workflowID, tenantIDForInsert(ctx), store.TeamWorkflowDeliveryPending, now, now)
	if err != nil {
		return nil, uuid.Nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, uuid.Nil, fmt.Errorf("workflow delivery is already claimed")
	}
	w, err := s.GetWorkflow(ctx, workflowID)
	return w, token, err
}

func (s *SQLiteTeamStore) CompleteWorkflowDelivery(ctx context.Context, workflowID, deliveryToken uuid.UUID) error {
	now := time.Now()
	res, err := s.db.ExecContext(ctx, `UPDATE team_workflows SET delivery_status=?,delivered_at=?,delivery_token=NULL,delivery_lease_until=NULL,updated_at=? WHERE id=? AND tenant_id=? AND delivery_token=? AND delivered_at IS NULL`, store.TeamWorkflowDeliveryDelivered, now, now, workflowID, tenantIDForInsert(ctx), deliveryToken)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("stale workflow delivery token")
	}
	return nil
}

func (s *SQLiteTeamStore) SearchWorkflows(ctx context.Context, teamID uuid.UUID, query string, limit int) ([]store.TeamWorkflowData, error) {
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	trimmed := strings.TrimSpace(query)
	rows, err := s.db.QueryContext(ctx, `SELECT `+workflowSelectCols+` FROM team_workflows WHERE tenant_id=? AND team_id=? AND (CAST(canonical_plan AS TEXT) LIKE ? OR plan_hash=?) ORDER BY created_at DESC LIMIT ?`, tenantIDForInsert(ctx), teamID, "%"+trimmed+"%", trimmed, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var workflows []store.TeamWorkflowData
	for rows.Next() {
		w, scanErr := scanSQLiteWorkflow(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		workflows = append(workflows, *w)
	}
	return workflows, rows.Err()
}

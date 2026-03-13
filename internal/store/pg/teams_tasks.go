package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// taskLockDuration is how long a claimed task stays locked before stale recovery resets it.
const taskLockDuration = 30 * time.Minute

// taskSelectCols is the shared SELECT column list for task queries (must match scanTaskRowsJoined).
const taskSelectCols = `t.id, t.team_id, t.subject, t.description, t.status, t.owner_agent_id, t.blocked_by, t.priority, t.result, t.user_id, t.channel,
		 t.task_type, t.task_number, COALESCE(t.identifier,''), t.created_by_agent_id, COALESCE(t.assignee_user_id,''), t.parent_id,
		 COALESCE(t.chat_id,''), t.locked_at, t.lock_expires_at, COALESCE(t.progress_percent,0), COALESCE(t.progress_step,''),
		 t.followup_at, COALESCE(t.followup_count,0), COALESCE(t.followup_max,0), COALESCE(t.followup_message,''), COALESCE(t.followup_channel,''), COALESCE(t.followup_chat_id,''),
		 t.created_at, t.updated_at,
		 COALESCE(a.agent_key, '') AS owner_agent_key,
		 COALESCE(ca.agent_key, '') AS created_by_agent_key`

// taskJoinClause is the shared JOIN clause for task queries.
const taskJoinClause = `FROM team_tasks t
		 LEFT JOIN agents a ON a.id = t.owner_agent_id
		 LEFT JOIN agents ca ON ca.id = t.created_by_agent_id`

// maxListTasksRows caps ListTasks results to prevent unbounded queries.
const maxListTasksRows = 200

// ============================================================
// Scopes
// ============================================================

func (s *PGTeamStore) ListTaskScopes(ctx context.Context, teamID uuid.UUID) ([]store.ScopeEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT channel, chat_id FROM team_tasks
		 WHERE team_id = $1 AND channel IS NOT NULL AND channel != ''
		 UNION
		 SELECT DISTINCT channel, chat_id FROM team_workspace_files
		 WHERE team_id = $1 AND archived_at IS NULL AND channel != ''
		 ORDER BY channel, chat_id`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scopes []store.ScopeEntry
	for rows.Next() {
		var s store.ScopeEntry
		if err := rows.Scan(&s.Channel, &s.ChatID); err != nil {
			return nil, err
		}
		scopes = append(scopes, s)
	}
	return scopes, rows.Err()
}

// ============================================================
// Tasks
// ============================================================

func (s *PGTeamStore) CreateTask(ctx context.Context, task *store.TeamTaskData) error {
	if task.ID == uuid.Nil {
		task.ID = store.GenNewID()
	}
	now := time.Now()
	task.CreatedAt = now
	task.UpdatedAt = now

	if task.TaskType == "" {
		task.TaskType = "general"
	}

	// Wrap entire operation in a transaction for atomicity.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Lock team row to serialize task_number generation (prevents races).
	if _, err := tx.ExecContext(ctx,
		`SELECT 1 FROM agent_teams WHERE id = $1 FOR UPDATE`, task.TeamID,
	); err != nil {
		return fmt.Errorf("lock team: %w", err)
	}

	var taskNumber int
	err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(task_number), 0) + 1 FROM team_tasks WHERE team_id = $1`,
		task.TeamID,
	).Scan(&taskNumber)
	if err != nil {
		return fmt.Errorf("compute task_number: %w", err)
	}
	task.TaskNumber = taskNumber

	// Generate identifier: UPPER(team_name[:3]) + "-" + task_number.
	prefix := "TSK"
	var teamName string
	if err := tx.QueryRowContext(ctx,
		`SELECT name FROM agent_teams WHERE id = $1`, task.TeamID,
	).Scan(&teamName); err == nil && teamName != "" {
		r := []rune(teamName)
		if len(r) > 3 {
			r = r[:3]
		}
		prefix = strings.ToUpper(string(r))
	}
	task.Identifier = fmt.Sprintf("%s-%d", prefix, taskNumber)

	// INSERT with all fields in one statement.
	_, err = tx.ExecContext(ctx,
		`INSERT INTO team_tasks (id, team_id, subject, description, status, owner_agent_id, blocked_by, priority, result, user_id, channel,
		 task_type, task_number, identifier, created_by_agent_id, parent_id, chat_id, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)`,
		task.ID, task.TeamID, task.Subject, task.Description,
		task.Status, task.OwnerAgentID, pq.Array(task.BlockedBy),
		task.Priority, task.Result,
		sql.NullString{String: task.UserID, Valid: task.UserID != ""},
		sql.NullString{String: task.Channel, Valid: task.Channel != ""},
		task.TaskType, taskNumber, task.Identifier,
		task.CreatedByAgentID, task.ParentID,
		sql.NullString{String: task.ChatID, Valid: task.ChatID != ""},
		now, now,
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// allowedTaskUpdateCols is the whitelist of columns that UpdateTask accepts.
var allowedTaskUpdateCols = map[string]bool{
	"subject":          true,
	"description":      true,
	"priority":         true,
	"assignee_user_id": true,
	"metadata":         true,
	"updated_at":       true,
}

func (s *PGTeamStore) UpdateTask(ctx context.Context, taskID uuid.UUID, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	// Validate columns against whitelist.
	for col := range updates {
		if !allowedTaskUpdateCols[col] {
			return fmt.Errorf("column %q is not allowed in task updates", col)
		}
	}
	updates["updated_at"] = time.Now()
	return execMapUpdate(ctx, s.db, "team_tasks", taskID, updates)
}

func (s *PGTeamStore) ListTasks(ctx context.Context, teamID uuid.UUID, orderBy string, statusFilter string, userID string, channel string, chatID string) ([]store.TeamTaskData, error) {
	orderClause := "t.priority DESC, t.created_at"
	if orderBy == "newest" {
		orderClause = "t.created_at DESC"
	}

	statusWhere := "AND t.status NOT IN ('completed','cancelled')" // default: active only
	switch statusFilter {
	case store.TeamTaskFilterAll:
		statusWhere = ""
	case store.TeamTaskFilterInReview:
		statusWhere = "AND t.status = 'in_review'"
	case store.TeamTaskFilterCompleted:
		statusWhere = "AND t.status IN ('completed','cancelled')"
	}

	// Scope filter: always bind $4/$5 but only enforce when non-empty.
	scopeWhere := "AND ($4 = '' OR COALESCE(t.channel,'') = $4) AND ($5 = '' OR COALESCE(t.chat_id,'') = $5)"

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE t.team_id = $1 AND ($2 = '' OR t.user_id = $2) `+statusWhere+` `+scopeWhere+`
		 ORDER BY `+orderClause+`
		 LIMIT $3`, teamID, userID, maxListTasksRows, channel, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

func (s *PGTeamStore) GetTask(ctx context.Context, taskID uuid.UUID) (*store.TeamTaskData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE t.id = $1`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks, err := scanTaskRowsJoined(rows)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, store.ErrTaskNotFound
	}
	return &tasks[0], nil
}

func (s *PGTeamStore) SearchTasks(ctx context.Context, teamID uuid.UUID, query string, limit int, userID string) ([]store.TeamTaskData, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE t.team_id = $1 AND t.tsv @@ plainto_tsquery('simple', $2) AND ($4 = '' OR t.user_id = $4)
		 ORDER BY ts_rank(t.tsv, plainto_tsquery('simple', $2)) DESC
		 LIMIT $3`, teamID, query, limit, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

func (s *PGTeamStore) ClaimTask(ctx context.Context, taskID, agentID, teamID uuid.UUID) error {
	now := time.Now()
	lockExpires := now.Add(taskLockDuration)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, owner_agent_id = $2, locked_at = $3, lock_expires_at = $4, updated_at = $3
		 WHERE id = $5 AND status = $6 AND owner_agent_id IS NULL AND team_id = $7`,
		store.TeamTaskStatusInProgress, agentID, now, lockExpires,
		taskID, store.TeamTaskStatusPending, teamID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not available for claiming (already claimed or not pending)")
	}
	return nil
}

func (s *PGTeamStore) AssignTask(ctx context.Context, taskID, agentID, teamID uuid.UUID) error {
	now := time.Now()
	lockExpires := now.Add(taskLockDuration)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, owner_agent_id = $2, locked_at = $3, lock_expires_at = $4, updated_at = $3
		 WHERE id = $5 AND team_id = $6 AND status = $7`,
		store.TeamTaskStatusInProgress, agentID, now, lockExpires,
		taskID, teamID, store.TeamTaskStatusPending,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not available for assignment (not pending or wrong team)")
	}
	return nil
}

func (s *PGTeamStore) CompleteTask(ctx context.Context, taskID, teamID uuid.UUID, result string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, result = $2, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 updated_at = $3
		 WHERE id = $4 AND status = $5 AND team_id = $6`,
		store.TeamTaskStatusCompleted, result, time.Now(),
		taskID, store.TeamTaskStatusInProgress, teamID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not in progress or not found")
	}

	if err := unblockDependentTasks(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PGTeamStore) CancelTask(ctx context.Context, taskID, teamID uuid.UUID, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, result = $2, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 updated_at = $3
		 WHERE id = $4 AND status NOT IN ($5, $1) AND team_id = $6`,
		store.TeamTaskStatusCancelled, reason, now,
		taskID, store.TeamTaskStatusCompleted, teamID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not found, already completed/cancelled, or wrong team")
	}

	if err := unblockDependentTasks(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PGTeamStore) FailTask(ctx context.Context, taskID, teamID uuid.UUID, errMsg string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, result = $2, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 updated_at = $3
		 WHERE id = $4 AND status = $5 AND team_id = $6`,
		store.TeamTaskStatusFailed, "FAILED: "+errMsg, now,
		taskID, store.TeamTaskStatusInProgress, teamID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not in progress or not found")
	}

	if err := unblockDependentTasks(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func scanTaskRowsJoined(rows *sql.Rows) ([]store.TeamTaskData, error) {
	var tasks []store.TeamTaskData
	for rows.Next() {
		var d store.TeamTaskData
		var desc, result, userID, channel sql.NullString
		var ownerID, createdByAgentID, parentID *uuid.UUID
		var blockedBy []uuid.UUID
		var assigneeUserID, chatID, progressStep, identifier string
		var lockedAt, lockExpiresAt, followupAt *time.Time
		var followupCount, followupMax int
		var followupMessage, followupChannel, followupChatID string
		if err := rows.Scan(
			&d.ID, &d.TeamID, &d.Subject, &desc, &d.Status,
			&ownerID, pq.Array(&blockedBy), &d.Priority, &result,
			&userID, &channel,
			&d.TaskType, &d.TaskNumber, &identifier, &createdByAgentID, &assigneeUserID, &parentID,
			&chatID, &lockedAt, &lockExpiresAt, &d.ProgressPercent, &progressStep,
			&followupAt, &followupCount, &followupMax, &followupMessage, &followupChannel, &followupChatID,
			&d.CreatedAt, &d.UpdatedAt,
			&d.OwnerAgentKey,
			&d.CreatedByAgentKey,
		); err != nil {
			return nil, err
		}
		if desc.Valid {
			d.Description = desc.String
		}
		if result.Valid {
			d.Result = &result.String
		}
		if userID.Valid {
			d.UserID = userID.String
		}
		if channel.Valid {
			d.Channel = channel.String
		}
		d.OwnerAgentID = ownerID
		d.BlockedBy = blockedBy
		d.Identifier = identifier
		d.CreatedByAgentID = createdByAgentID
		d.AssigneeUserID = assigneeUserID
		d.ParentID = parentID
		d.ChatID = chatID
		d.LockedAt = lockedAt
		d.LockExpiresAt = lockExpiresAt
		d.ProgressStep = progressStep
		d.FollowupAt = followupAt
		d.FollowupCount = followupCount
		d.FollowupMax = followupMax
		d.FollowupMessage = followupMessage
		d.FollowupChannel = followupChannel
		d.FollowupChatID = followupChatID
		tasks = append(tasks, d)
	}
	return tasks, rows.Err()
}

// unblockDependentTasks removes taskID from blocked_by arrays and transitions blocked→pending
// when all blockers are resolved. Must be called within a transaction.
func unblockDependentTasks(ctx context.Context, tx *sql.Tx, taskID uuid.UUID) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET
		   blocked_by = array_remove(blocked_by, $1),
		   status = CASE WHEN status = 'blocked' AND array_length(array_remove(blocked_by, $1), 1) IS NULL THEN 'pending' ELSE status END,
		   updated_at = $2
		 WHERE $1 = ANY(blocked_by)`,
		taskID, time.Now(),
	)
	return err
}

// ============================================================
// Review workflow
// ============================================================

func (s *PGTeamStore) ReviewTask(ctx context.Context, taskID, teamID uuid.UUID) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, updated_at = $2
		 WHERE id = $3 AND status = $4 AND team_id = $5`,
		store.TeamTaskStatusInReview, time.Now(),
		taskID, store.TeamTaskStatusInProgress, teamID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not in progress or not found")
	}
	return nil
}

func (s *PGTeamStore) ApproveTask(ctx context.Context, taskID, teamID uuid.UUID, comment string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 updated_at = $2
		 WHERE id = $3 AND status = $4 AND team_id = $5`,
		store.TeamTaskStatusCompleted, now,
		taskID, store.TeamTaskStatusInReview, teamID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not in review or not found")
	}

	if err := unblockDependentTasks(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *PGTeamStore) RejectTask(ctx context.Context, taskID, teamID uuid.UUID, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	res, err := tx.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, result = $2, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 updated_at = $3
		 WHERE id = $4 AND status = $5 AND team_id = $6`,
		store.TeamTaskStatusCancelled, reason, now,
		taskID, store.TeamTaskStatusInReview, teamID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not in review or not found")
	}

	if err := unblockDependentTasks(ctx, tx, taskID); err != nil {
		return err
	}
	return tx.Commit()
}

// ============================================================
// Task comments
// ============================================================

func (s *PGTeamStore) AddTaskComment(ctx context.Context, comment *store.TeamTaskCommentData) error {
	if comment.ID == uuid.Nil {
		comment.ID = store.GenNewID()
	}
	comment.CreatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_task_comments (id, task_id, agent_id, user_id, content, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		comment.ID, comment.TaskID, comment.AgentID,
		sql.NullString{String: comment.UserID, Valid: comment.UserID != ""},
		comment.Content, comment.CreatedAt,
	)
	return err
}

func (s *PGTeamStore) ListTaskComments(ctx context.Context, taskID uuid.UUID) ([]store.TeamTaskCommentData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.task_id, c.agent_id, c.user_id, c.content, c.created_at,
		 COALESCE(a.agent_key, '') AS agent_key
		 FROM team_task_comments c
		 LEFT JOIN agents a ON a.id = c.agent_id
		 WHERE c.task_id = $1
		 ORDER BY c.created_at ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []store.TeamTaskCommentData
	for rows.Next() {
		var c store.TeamTaskCommentData
		var agentID *uuid.UUID
		var userID sql.NullString
		if err := rows.Scan(&c.ID, &c.TaskID, &agentID, &userID, &c.Content, &c.CreatedAt, &c.AgentKey); err != nil {
			return nil, err
		}
		c.AgentID = agentID
		if userID.Valid {
			c.UserID = userID.String
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

// ============================================================
// Audit events
// ============================================================

func (s *PGTeamStore) RecordTaskEvent(ctx context.Context, event *store.TeamTaskEventData) error {
	if event.ID == uuid.Nil {
		event.ID = store.GenNewID()
	}
	event.CreatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_task_events (id, task_id, event_type, actor_type, actor_id, data, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		event.ID, event.TaskID, event.EventType, event.ActorType, event.ActorID, event.Data, event.CreatedAt,
	)
	return err
}

func (s *PGTeamStore) ListTaskEvents(ctx context.Context, taskID uuid.UUID) ([]store.TeamTaskEventData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id, event_type, actor_type, actor_id, data, created_at
		 FROM team_task_events
		 WHERE task_id = $1
		 ORDER BY created_at ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []store.TeamTaskEventData
	for rows.Next() {
		var e store.TeamTaskEventData
		var data json.RawMessage
		if err := rows.Scan(&e.ID, &e.TaskID, &e.EventType, &e.ActorType, &e.ActorID, &data, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Data = data
		events = append(events, e)
	}
	return events, rows.Err()
}

// ============================================================
// Attachments
// ============================================================

func (s *PGTeamStore) AttachFileToTask(ctx context.Context, att *store.TeamTaskAttachmentData) error {
	if att.ID == uuid.Nil {
		att.ID = store.GenNewID()
	}
	att.CreatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_task_attachments (id, task_id, file_id, added_by, created_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (task_id, file_id) DO NOTHING`,
		att.ID, att.TaskID, att.FileID, att.AddedBy, att.CreatedAt,
	)
	return err
}

func (s *PGTeamStore) ListTaskAttachments(ctx context.Context, taskID uuid.UUID) ([]store.TeamTaskAttachmentData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT a.id, a.task_id, a.file_id, a.added_by, a.created_at,
		 COALESCE(f.file_name, '') AS file_name
		 FROM team_task_attachments a
		 LEFT JOIN team_workspace_files f ON f.id = a.file_id
		 WHERE a.task_id = $1
		 ORDER BY a.created_at ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var atts []store.TeamTaskAttachmentData
	for rows.Next() {
		var a store.TeamTaskAttachmentData
		var addedBy *uuid.UUID
		if err := rows.Scan(&a.ID, &a.TaskID, &a.FileID, &addedBy, &a.CreatedAt, &a.FileName); err != nil {
			return nil, err
		}
		a.AddedBy = addedBy
		atts = append(atts, a)
	}
	return atts, rows.Err()
}

func (s *PGTeamStore) DetachFileFromTask(ctx context.Context, taskID, fileID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM team_task_attachments WHERE task_id = $1 AND file_id = $2`,
		taskID, fileID,
	)
	return err
}

// ============================================================
// Progress
// ============================================================

func (s *PGTeamStore) UpdateTaskProgress(ctx context.Context, taskID, teamID uuid.UUID, percent int, step string) error {
	if percent < 0 || percent > 100 {
		return fmt.Errorf("progress percent must be 0-100, got %d", percent)
	}
	// Also renews lock_expires_at as a heartbeat.
	now := time.Now()
	lockExpires := now.Add(taskLockDuration)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET progress_percent = $1, progress_step = $2, lock_expires_at = $3, updated_at = $4
		 WHERE id = $5 AND status = $6 AND team_id = $7`,
		percent, step, lockExpires, now,
		taskID, store.TeamTaskStatusInProgress, teamID,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not in progress or not found")
	}
	return nil
}

// ============================================================
// Stale recovery
// ============================================================

func (s *PGTeamStore) RecoverStaleTasks(ctx context.Context, teamID uuid.UUID) (int, error) {
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, owner_agent_id = NULL, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 updated_at = $2
		 WHERE team_id = $3 AND status = $4 AND lock_expires_at IS NOT NULL AND lock_expires_at < $2`,
		store.TeamTaskStatusPending, now,
		teamID, store.TeamTaskStatusInProgress,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func (s *PGTeamStore) ForceRecoverAllTasks(ctx context.Context, teamID uuid.UUID) (int, error) {
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = $1, owner_agent_id = NULL, locked_at = NULL, lock_expires_at = NULL,
		 followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL,
		 updated_at = $2
		 WHERE team_id = $3 AND status = $4`,
		store.TeamTaskStatusPending, now,
		teamID, store.TeamTaskStatusInProgress,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func (s *PGTeamStore) ListRecoverableTasks(ctx context.Context, teamID uuid.UUID) ([]store.TeamTaskData, error) {
	now := time.Now()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE t.team_id = $1
		   AND (
		     t.status = $2
		     OR (t.status = $3 AND t.lock_expires_at IS NOT NULL AND t.lock_expires_at < $4)
		   )
		 ORDER BY t.priority DESC, t.created_at
		 LIMIT $5`,
		teamID, store.TeamTaskStatusPending, store.TeamTaskStatusInProgress, now, maxListTasksRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

// ============================================================
// Follow-up reminders
// ============================================================

func (s *PGTeamStore) SetTaskFollowup(ctx context.Context, taskID, teamID uuid.UUID, followupAt time.Time, max int, message, channel, chatID string) error {
	now := time.Now()
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET followup_at = $1, followup_max = $2, followup_message = $3, followup_channel = $4, followup_chat_id = $5, updated_at = $6
		 WHERE id = $7 AND team_id = $8 AND status = $9`,
		followupAt, max, message, channel, chatID, now,
		taskID, teamID, store.TeamTaskStatusInProgress,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not in progress or not found")
	}
	return nil
}

func (s *PGTeamStore) ClearTaskFollowup(ctx context.Context, taskID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL, updated_at = $1
		 WHERE id = $2`,
		time.Now(), taskID,
	)
	return err
}

func (s *PGTeamStore) ListFollowupDueTasks(ctx context.Context, teamID uuid.UUID) ([]store.TeamTaskData, error) {
	now := time.Now()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE t.team_id = $1
		   AND t.followup_at IS NOT NULL
		   AND t.followup_at <= $2
		   AND t.status = $3
		   AND (t.followup_max = 0 OR t.followup_count < t.followup_max)
		 ORDER BY t.followup_at
		 LIMIT 50`,
		teamID, now, store.TeamTaskStatusInProgress,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

func (s *PGTeamStore) IncrementFollowupCount(ctx context.Context, taskID uuid.UUID, nextAt *time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET followup_count = followup_count + 1, followup_at = $1, updated_at = $2
		 WHERE id = $3`,
		nextAt, time.Now(), taskID,
	)
	return err
}

func (s *PGTeamStore) ClearFollowupByScope(ctx context.Context, channel, chatID string) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks
		 SET followup_at = NULL, followup_count = 0, followup_message = NULL,
		     followup_channel = NULL, followup_chat_id = NULL, updated_at = NOW()
		 WHERE followup_channel = $1 AND followup_chat_id = $2
		   AND followup_at IS NOT NULL AND status = $3`,
		channel, chatID, store.TeamTaskStatusInProgress,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (s *PGTeamStore) SetFollowupForActiveTasks(ctx context.Context, teamID uuid.UUID, channel, chatID string, followupAt time.Time, max int, message string) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks
		 SET followup_at = $4, followup_max = $5, followup_message = $6,
		     followup_channel = $2, followup_chat_id = $3, updated_at = NOW()
		 WHERE team_id = $1
		   AND status = $7
		   AND followup_at IS NULL
		   AND (
		     (COALESCE(channel,'') = $2 AND COALESCE(chat_id,'') = $3)
		     OR followup_channel = $2
		     OR (COALESCE(channel,'') IN ('', 'system', 'delegate') AND COALESCE(chat_id,'') = '')
		   )`,
		teamID, channel, chatID, followupAt, max, message, store.TeamTaskStatusInProgress,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

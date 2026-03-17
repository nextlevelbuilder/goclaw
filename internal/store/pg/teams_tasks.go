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
		 COALESCE(t.chat_id,''), t.metadata, t.locked_at, t.lock_expires_at, COALESCE(t.progress_percent,0), COALESCE(t.progress_step,''),
		 t.followup_at, COALESCE(t.followup_count,0), COALESCE(t.followup_max,0), COALESCE(t.followup_message,''), COALESCE(t.followup_channel,''), COALESCE(t.followup_chat_id,''),
		 t.created_at, t.updated_at,
		 COALESCE(a.agent_key, '') AS owner_agent_key,
		 COALESCE(ca.agent_key, '') AS created_by_agent_key`

// taskJoinClause is the shared JOIN clause for task queries.
const taskJoinClause = `FROM team_tasks t
		 LEFT JOIN agents a ON a.id = t.owner_agent_id
		 LEFT JOIN agents ca ON ca.id = t.created_by_agent_id`

// maxListTasksRows caps ListTasks results to prevent unbounded queries.
const maxListTasksRows = 30

// ============================================================
// Scopes
// ============================================================

func (s *PGTeamStore) ListTaskScopes(ctx context.Context, teamID uuid.UUID) ([]store.ScopeEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT channel, chat_id FROM team_tasks
		 WHERE team_id = $1 AND channel IS NOT NULL AND channel != ''
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

	// Generate identifier: T-{taskNumber}-{last 4 hex of UUID}.
	// Sequential via taskNumber, unique via UUID suffix. No extra DB query needed.
	hex := strings.ReplaceAll(task.ID.String(), "-", "")
	task.Identifier = fmt.Sprintf("T-%03d-%s", taskNumber, hex[len(hex)-4:])

	// Serialize metadata to JSON (NULL when empty).
	var metaJSON []byte
	if len(task.Metadata) > 0 {
		metaJSON, _ = json.Marshal(task.Metadata)
	}

	// INSERT with all fields in one statement.
	_, err = tx.ExecContext(ctx,
		`INSERT INTO team_tasks (id, team_id, subject, description, status, owner_agent_id, blocked_by, priority, result, user_id, channel,
		 task_type, task_number, identifier, created_by_agent_id, parent_id, chat_id, metadata, locked_at, lock_expires_at, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)`,
		task.ID, task.TeamID, task.Subject, task.Description,
		task.Status, task.OwnerAgentID, pq.Array(task.BlockedBy),
		task.Priority, task.Result,
		sql.NullString{String: task.UserID, Valid: task.UserID != ""},
		sql.NullString{String: task.Channel, Valid: task.Channel != ""},
		task.TaskType, taskNumber, task.Identifier,
		task.CreatedByAgentID, task.ParentID,
		sql.NullString{String: task.ChatID, Valid: task.ChatID != ""},
		metaJSON,
		task.LockedAt, task.LockExpiresAt,
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
	"blocked_by":       true,
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
	// Wrap blocked_by slice with pq.Array for PostgreSQL array column.
	if v, ok := updates["blocked_by"]; ok {
		updates["blocked_by"] = pq.Array(v)
	}
	updates["updated_at"] = time.Now()
	return execMapUpdate(ctx, s.db, "team_tasks", taskID, updates)
}

func (s *PGTeamStore) ListTasks(ctx context.Context, teamID uuid.UUID, orderBy string, statusFilter string, userID string, channel string, chatID string, offset int) ([]store.TeamTaskData, error) {
	orderClause := "t.priority DESC, t.created_at"
	if orderBy == "newest" {
		orderClause = "t.created_at DESC"
	}

	statusWhere := "" // default: all statuses (no filter)
	switch statusFilter {
	case store.TeamTaskFilterActive:
		statusWhere = "AND t.status NOT IN ('completed','cancelled')"
	case store.TeamTaskFilterInReview:
		statusWhere = "AND t.status = 'in_review'"
	case store.TeamTaskFilterCompleted:
		statusWhere = "AND t.status IN ('completed','cancelled')"
	// "", store.TeamTaskFilterAll ("all") → no filter (all statuses)
	}

	// Scope filter: always bind $4/$5 but only enforce when non-empty.
	scopeWhere := "AND ($4 = '' OR COALESCE(t.channel,'') = $4) AND ($5 = '' OR COALESCE(t.chat_id,'') = $5)"

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE t.team_id = $1 AND ($2 = '' OR t.user_id = $2) `+statusWhere+` `+scopeWhere+`
		 ORDER BY `+orderClause+`
		 LIMIT $3 OFFSET $6`, teamID, userID, maxListTasksRows+1, channel, chatID, offset)
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

func (s *PGTeamStore) GetTasksByIDs(ctx context.Context, ids []uuid.UUID) ([]store.TeamTaskData, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE t.id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
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

func (s *PGTeamStore) DeleteTask(ctx context.Context, taskID, teamID uuid.UUID) error {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM team_tasks WHERE id = $1 AND team_id = $2 AND status IN ('completed','failed','cancelled')`,
		taskID, teamID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrTaskNotFound
	}
	return nil
}

func (s *PGTeamStore) DeleteTasks(ctx context.Context, taskIDs []uuid.UUID, teamID uuid.UUID) ([]uuid.UUID, error) {
	if len(taskIDs) == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`DELETE FROM team_tasks
		 WHERE id = ANY($1) AND team_id = $2 AND status IN ('completed','failed','cancelled')
		 RETURNING id`,
		taskIDs, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deleted []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return deleted, err
		}
		deleted = append(deleted, id)
	}
	return deleted, rows.Err()
}

func scanTaskRowsJoined(rows *sql.Rows) ([]store.TeamTaskData, error) {
	var tasks []store.TeamTaskData
	for rows.Next() {
		var d store.TeamTaskData
		var desc, result, userID, channel sql.NullString
		var ownerID, createdByAgentID, parentID *uuid.UUID
		var blockedBy []uuid.UUID
		var assigneeUserID, chatID, progressStep, identifier string
		var metadataJSON []byte
		var lockedAt, lockExpiresAt, followupAt *time.Time
		var followupCount, followupMax int
		var followupMessage, followupChannel, followupChatID string
		if err := rows.Scan(
			&d.ID, &d.TeamID, &d.Subject, &desc, &d.Status,
			&ownerID, pq.Array(&blockedBy), &d.Priority, &result,
			&userID, &channel,
			&d.TaskType, &d.TaskNumber, &identifier, &createdByAgentID, &assigneeUserID, &parentID,
			&chatID, &metadataJSON, &lockedAt, &lockExpiresAt, &d.ProgressPercent, &progressStep,
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
		if len(metadataJSON) > 0 {
			_ = json.Unmarshal(metadataJSON, &d.Metadata)
		}
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

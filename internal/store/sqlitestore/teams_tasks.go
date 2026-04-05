//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// taskLockDuration is how long a claimed task stays locked before stale recovery resets it.
const taskLockDuration = 30 * time.Minute

// maxListTasksRows caps ListTasks results to prevent unbounded queries.
const maxListTasksRows = 30

// taskSelectCols is the shared SELECT column list for task queries.
const taskSelectCols = `t.id, t.team_id, t.subject, t.description, t.status, t.owner_agent_id, t.blocked_by, t.priority, t.result, t.user_id, t.channel,
		 t.task_type, t.task_number, COALESCE(t.identifier,''), t.created_by_agent_id, COALESCE(t.assignee_user_id,''), t.parent_id,
		 COALESCE(t.chat_id,''), t.metadata, t.locked_at, t.lock_expires_at, COALESCE(t.progress_percent,0), COALESCE(t.progress_step,''),
		 t.followup_at, COALESCE(t.followup_count,0), COALESCE(t.followup_max,0), COALESCE(t.followup_message,''), COALESCE(t.followup_channel,''), COALESCE(t.followup_chat_id,''),
		 COALESCE(t.comment_count,0), COALESCE(t.attachment_count,0),
		 t.created_at, t.updated_at,
		 COALESCE(a.agent_key, '') AS owner_agent_key,
		 COALESCE(ca.agent_key, '') AS created_by_agent_key`

// taskJoinClause is the shared JOIN clause for task queries.
const taskJoinClause = `FROM team_tasks t
		 LEFT JOIN agents a ON a.id = t.owner_agent_id
		 LEFT JOIN agents ca ON ca.id = t.created_by_agent_id`

// ============================================================
// Scopes
// ============================================================

func (s *SQLiteTeamStore) ListTaskScopes(ctx context.Context, teamID uuid.UUID) ([]store.ScopeEntry, error) {
	args := []any{teamID}
	tenantWhere := ""
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required")
		}
		tenantWhere = " AND tenant_id = ?"
		args = append(args, tid)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT channel, chat_id FROM team_tasks
		 WHERE team_id = ? AND channel IS NOT NULL AND channel != ''`+tenantWhere+`
		 ORDER BY channel, chat_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var scopes []store.ScopeEntry
	for rows.Next() {
		var sc store.ScopeEntry
		if err := rows.Scan(&sc.Channel, &sc.ChatID); err != nil {
			return nil, err
		}
		scopes = append(scopes, sc)
	}
	return scopes, rows.Err()
}

// ============================================================
// Tasks CRUD
// ============================================================

func (s *SQLiteTeamStore) CreateTask(ctx context.Context, task *store.TeamTaskData) error {
	if task.ID == uuid.Nil {
		task.ID = store.GenNewID()
	}
	now := time.Now()
	task.CreatedAt = now
	task.UpdatedAt = now

	if task.TaskType == "" {
		task.TaskType = "general"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Scope task_number per (team_id, chat_id).
	var taskNumber int
	err = tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(task_number), 0) + 1 FROM team_tasks WHERE team_id = ? AND COALESCE(chat_id, '') = ?`,
		task.TeamID, task.ChatID,
	).Scan(&taskNumber)
	if err != nil {
		return fmt.Errorf("compute task_number: %w", err)
	}
	task.TaskNumber = taskNumber

	hex := strings.ReplaceAll(task.ID.String(), "-", "")
	task.Identifier = fmt.Sprintf("T-%03d-%s", taskNumber, hex[len(hex)-4:])

	var metaJSON []byte
	if len(task.Metadata) > 0 {
		metaJSON, _ = json.Marshal(task.Metadata)
	}

	blockedByJSON := jsonStringArray(uuidSliceToStrings(task.BlockedBy))

	_, err = tx.ExecContext(ctx,
		`INSERT INTO team_tasks (id, team_id, subject, description, status, owner_agent_id, blocked_by, priority, result, user_id, channel,
		 task_type, task_number, identifier, created_by_agent_id, parent_id, chat_id, metadata, locked_at, lock_expires_at, created_at, updated_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.TeamID, task.Subject, task.Description,
		task.Status, task.OwnerAgentID, blockedByJSON,
		task.Priority, task.Result,
		nilStr(task.UserID), nilStr(task.Channel),
		task.TaskType, taskNumber, task.Identifier,
		task.CreatedByAgentID, task.ParentID,
		nilStr(task.ChatID),
		metaJSON,
		task.LockedAt, task.LockExpiresAt,
		now, now, tenantIDForInsert(ctx),
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

func (s *SQLiteTeamStore) UpdateTask(ctx context.Context, taskID uuid.UUID, updates map[string]any) error {
	if len(updates) == 0 {
		return nil
	}
	for col := range updates {
		if !allowedTaskUpdateCols[col] {
			return fmt.Errorf("column %q is not allowed in task updates", col)
		}
	}
	// Convert blocked_by []uuid.UUID to JSON string array for SQLite.
	if v, ok := updates["blocked_by"]; ok {
		switch tv := v.(type) {
		case []uuid.UUID:
			updates["blocked_by"] = jsonStringArray(uuidSliceToStrings(tv))
		case []string:
			updates["blocked_by"] = jsonStringArray(tv)
		}
	}
	updates["updated_at"] = time.Now()
	if store.IsCrossTenant(ctx) {
		return execMapUpdate(ctx, s.db, "team_tasks", taskID, updates)
	}
	tid := store.TenantIDFromContext(ctx)
	if tid == uuid.Nil {
		return fmt.Errorf("tenant_id required for update")
	}
	return execMapUpdateWhereTenant(ctx, s.db, "team_tasks", updates, taskID, tid)
}

func (s *SQLiteTeamStore) ListTasks(ctx context.Context, teamID uuid.UUID, orderBy string, statusFilter string, userID string, channel string, chatID string, limit int, offset int) ([]store.TeamTaskData, error) {
	orderClause := "t.priority DESC, t.created_at"
	if orderBy == "newest" {
		orderClause = "t.created_at DESC"
	}

	statusWhere := ""
	switch statusFilter {
	case store.TeamTaskFilterActive:
		statusWhere = "AND t.status NOT IN ('completed','cancelled')"
	case store.TeamTaskFilterInReview:
		statusWhere = "AND t.status = 'in_review'"
	case store.TeamTaskFilterCompleted:
		statusWhere = "AND t.status IN ('completed','cancelled')"
	}

	if limit <= 0 {
		limit = maxListTasksRows
	}

	// Scope filter using COALESCE for optional channel/chatID.
	scopeWhere := "AND (? = '' OR COALESCE(t.channel,'') = ?) AND (? = '' OR COALESCE(t.chat_id,'') = ?)"

	args := []any{teamID, userID, userID, channel, channel, chatID, chatID}

	tenantWhere := ""
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required")
		}
		tenantWhere = " AND t.tenant_id = ?"
		args = append(args, tid)
	}
	args = append(args, limit+1, offset)

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE t.team_id = ? AND (? = '' OR t.user_id = ?) `+statusWhere+` `+scopeWhere+tenantWhere+`
		 ORDER BY `+orderClause+`
		 LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

func (s *SQLiteTeamStore) GetTask(ctx context.Context, taskID uuid.UUID) (*store.TeamTaskData, error) {
	args := []any{taskID}
	tenantWhere := ""
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required")
		}
		tenantWhere = " AND t.tenant_id = ?"
		args = append(args, tid)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE t.id = ?`+tenantWhere, args...)
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

func (s *SQLiteTeamStore) GetTasksByIDs(ctx context.Context, ids []uuid.UUID) ([]store.TeamTaskData, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := strings.Repeat("?,", len(ids))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	tenantWhere := ""
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required")
		}
		tenantWhere = " AND t.tenant_id = ?"
		args = append(args, tid)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE t.id IN (`+placeholders+`)`+tenantWhere, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

// SearchTasks uses LIKE-based search (SQLite has no tsvector/FTS without extension).
func (s *SQLiteTeamStore) SearchTasks(ctx context.Context, teamID uuid.UUID, query string, limit int, userID string) ([]store.TeamTaskData, error) {
	if limit <= 0 {
		limit = 20
	}
	words := strings.Fields(query)
	if len(words) == 0 {
		return nil, nil
	}
	// Sanitize words.
	var sanitized []string
	for _, w := range words {
		w = strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
				return r
			}
			return -1
		}, w)
		w = strings.TrimSpace(w)
		if w != "" {
			sanitized = append(sanitized, "%"+w+"%")
		}
	}
	if len(sanitized) == 0 {
		return nil, nil
	}

	// Build AND-chained LIKE conditions.
	likeClauses := make([]string, len(sanitized))
	args := []any{teamID}
	for i, pat := range sanitized {
		likeClauses[i] = "(t.subject LIKE ? OR t.description LIKE ?)"
		args = append(args, pat, pat)
	}
	args = append(args, userID, userID)

	tenantWhere := ""
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required")
		}
		tenantWhere = " AND t.tenant_id = ?"
		args = append(args, tid)
	}
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE t.team_id = ? AND `+strings.Join(likeClauses, " AND ")+`
		   AND (? = '' OR t.user_id = ?)`+tenantWhere+`
		 ORDER BY t.created_at DESC
		 LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

func (s *SQLiteTeamStore) DeleteTask(ctx context.Context, taskID, teamID uuid.UUID) error {
	args := []any{taskID, teamID}
	tenantWhere := ""
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return fmt.Errorf("tenant_id required")
		}
		tenantWhere = " AND tenant_id = ?"
		args = append(args, tid)
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM team_tasks WHERE id = ? AND team_id = ? AND status IN ('completed','failed','cancelled')`+tenantWhere,
		args...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrTaskNotFound
	}
	return nil
}

func (s *SQLiteTeamStore) DeleteTasks(ctx context.Context, taskIDs []uuid.UUID, teamID uuid.UUID) ([]uuid.UUID, error) {
	if len(taskIDs) == 0 {
		return nil, nil
	}
	// SQLite doesn't support RETURNING in older versions — do select then delete.
	placeholders := strings.Repeat("?,", len(taskIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, len(taskIDs))
	for i, id := range taskIDs {
		args[i] = id
	}
	args = append(args, teamID)

	tenantWhere := ""
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required")
		}
		tenantWhere = " AND tenant_id = ?"
		args = append(args, tid)
	}

	cond := `id IN (` + placeholders + `) AND team_id = ? AND status IN ('completed','failed','cancelled')` + tenantWhere

	// Fetch IDs to delete first.
	selectRows, err := s.db.QueryContext(ctx, `SELECT id FROM team_tasks WHERE `+cond, args...)
	if err != nil {
		return nil, err
	}
	var deleted []uuid.UUID
	for selectRows.Next() {
		var id uuid.UUID
		if err := selectRows.Scan(&id); err != nil {
			selectRows.Close()
			return deleted, err
		}
		deleted = append(deleted, id)
	}
	selectRows.Close()
	if err := selectRows.Err(); err != nil {
		return nil, err
	}

	if len(deleted) > 0 {
		_, err = s.db.ExecContext(ctx, `DELETE FROM team_tasks WHERE `+cond, args...)
		if err != nil {
			return nil, err
		}
	}
	return deleted, nil
}

func (s *SQLiteTeamStore) ListActiveTasksByChatID(ctx context.Context, chatID string) ([]store.TeamTaskData, error) {
	if chatID == "" {
		return nil, nil
	}
	args := []any{chatID}
	tenantWhere := ""
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			return nil, fmt.Errorf("tenant_id required")
		}
		tenantWhere = " AND t.tenant_id = ?"
		args = append(args, tid)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE COALESCE(t.chat_id,'') = ?
		   AND t.status IN ('pending','in_progress','blocked','in_review')`+tenantWhere+`
		 ORDER BY t.task_number ASC
		 LIMIT 50`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

// ============================================================
// Scan helpers
// ============================================================

func scanTaskRowsJoined(rows *sql.Rows) ([]store.TeamTaskData, error) {
	var tasks []store.TeamTaskData
	for rows.Next() {
		var d store.TeamTaskData
		var desc, result, userID, channel sql.NullString
		var ownerID, createdByAgentID, parentID *uuid.UUID
		var blockedByJSON []byte
		var assigneeUserID, chatID, progressStep, identifier string
		var metadataJSON []byte
		var lockedAt, lockExpiresAt, followupAt nullSqliteTime
		var followupCount, followupMax int
		var followupMessage, followupChannel, followupChatID string
		createdAt, updatedAt := scanTimePair()
		if err := rows.Scan(
			&d.ID, &d.TeamID, &d.Subject, &desc, &d.Status,
			&ownerID, &blockedByJSON, &d.Priority, &result,
			&userID, &channel,
			&d.TaskType, &d.TaskNumber, &identifier, &createdByAgentID, &assigneeUserID, &parentID,
			&chatID, &metadataJSON, &lockedAt, &lockExpiresAt, &d.ProgressPercent, &progressStep,
			&followupAt, &followupCount, &followupMax, &followupMessage, &followupChannel, &followupChatID,
			&d.CommentCount, &d.AttachmentCount,
			createdAt, updatedAt,
			&d.OwnerAgentKey,
			&d.CreatedByAgentKey,
		); err != nil {
			return nil, err
		}
		d.CreatedAt = createdAt.Time
		d.UpdatedAt = updatedAt.Time
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
		d.Identifier = identifier
		d.CreatedByAgentID = createdByAgentID
		d.AssigneeUserID = assigneeUserID
		d.ParentID = parentID
		d.ChatID = chatID

		// Decode blocked_by JSON string array → []uuid.UUID.
		var blockedByStrs []string
		scanJSONStringArray(blockedByJSON, &blockedByStrs)
		d.BlockedBy = stringsToUUIDSlice(blockedByStrs)

		if len(metadataJSON) > 0 {
			_ = json.Unmarshal(metadataJSON, &d.Metadata)
		}
		if lockedAt.Valid {
			d.LockedAt = &lockedAt.Time
		}
		if lockExpiresAt.Valid {
			d.LockExpiresAt = &lockExpiresAt.Time
		}
		d.ProgressStep = progressStep
		if followupAt.Valid {
			d.FollowupAt = &followupAt.Time
		}
		d.FollowupCount = followupCount
		d.FollowupMax = followupMax
		d.FollowupMessage = followupMessage
		d.FollowupChannel = followupChannel
		d.FollowupChatID = followupChatID
		tasks = append(tasks, d)
	}
	return tasks, rows.Err()
}

// uuidSliceToStrings converts []uuid.UUID to []string for JSON storage.
func uuidSliceToStrings(ids []uuid.UUID) []string {
	if ids == nil {
		return nil
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// stringsToUUIDSlice converts []string back to []uuid.UUID.
func stringsToUUIDSlice(strs []string) []uuid.UUID {
	if strs == nil {
		return nil
	}
	out := make([]uuid.UUID, 0, len(strs))
	for _, s := range strs {
		if id, err := uuid.Parse(s); err == nil {
			out = append(out, id)
		}
	}
	return out
}

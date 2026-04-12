//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ============================================================
// Task comments
// ============================================================

func (s *SQLiteTeamStore) AddTaskComment(ctx context.Context, comment *store.TeamTaskCommentData) error {
	if comment.ID == uuid.Nil {
		comment.ID = store.GenNewID()
	}
	comment.CreatedAt = time.Now()
	commentType := comment.CommentType
	if commentType == "" {
		commentType = "note"
	}
	tid := tenantIDForInsert(ctx)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_task_comments (id, task_id, agent_id, user_id, content, comment_type, created_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		comment.ID, comment.TaskID, comment.AgentID,
		nilStr(comment.UserID),
		comment.Content, commentType, comment.CreatedAt, tid,
	)
	if err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx,
		`UPDATE team_tasks SET comment_count = comment_count + 1 WHERE id = ? AND tenant_id = ?`, comment.TaskID, tid)
	return nil
}

func (s *SQLiteTeamStore) ListTaskComments(ctx context.Context, taskID uuid.UUID) ([]store.TeamTaskCommentData, error) {
	tid := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.task_id, c.agent_id, c.user_id, c.content, c.comment_type, c.created_at,
		 COALESCE(a.agent_key, '') AS agent_key
		 FROM team_task_comments c
		 LEFT JOIN agents a ON a.id = c.agent_id
		 WHERE c.task_id = ? AND c.tenant_id = ?
		 ORDER BY c.created_at ASC`, taskID, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskCommentRows(rows)
}

func (s *SQLiteTeamStore) ListRecentTaskComments(ctx context.Context, taskID uuid.UUID, limit int) ([]store.TeamTaskCommentData, error) {
	tid := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.task_id, c.agent_id, c.user_id, c.content, c.comment_type, c.created_at,
		 COALESCE(a.agent_key, '') AS agent_key
		 FROM team_task_comments c
		 LEFT JOIN agents a ON a.id = c.agent_id
		 WHERE c.task_id = ? AND c.tenant_id = ?
		 ORDER BY c.created_at DESC
		 LIMIT ?`, taskID, tid, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	comments, err := scanTaskCommentRows(rows)
	if err != nil {
		return nil, err
	}
	// Reverse to chronological order (ASC) for display.
	for i, j := 0, len(comments)-1; i < j; i, j = i+1, j-1 {
		comments[i], comments[j] = comments[j], comments[i]
	}
	return comments, nil
}

func scanTaskCommentRows(rows *sql.Rows) ([]store.TeamTaskCommentData, error) {
	var comments []store.TeamTaskCommentData
	for rows.Next() {
		var c store.TeamTaskCommentData
		var agentID *uuid.UUID
		var userID sql.NullString
		var createdAt sqliteTime
		if err := rows.Scan(&c.ID, &c.TaskID, &agentID, &userID, &c.Content, &c.CommentType, &createdAt, &c.AgentKey); err != nil {
			return nil, err
		}
		c.CreatedAt = createdAt.Time
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

func (s *SQLiteTeamStore) RecordTaskEvent(ctx context.Context, event *store.TeamTaskEventData) error {
	if event.ID == uuid.Nil {
		event.ID = store.GenNewID()
	}
	event.CreatedAt = time.Now()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_task_events (id, task_id, event_type, actor_type, actor_id, data, created_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.TaskID, event.EventType, event.ActorType, event.ActorID, event.Data, event.CreatedAt, tenantIDForInsert(ctx),
	)
	return err
}

func (s *SQLiteTeamStore) ListTaskEvents(ctx context.Context, taskID uuid.UUID) ([]store.TeamTaskEventData, error) {
	tid := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id, event_type, actor_type, actor_id, data, created_at
		 FROM team_task_events
		 WHERE task_id = ? AND tenant_id = ?
		 ORDER BY created_at ASC`, taskID, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskEventRows(rows)
}

func (s *SQLiteTeamStore) ListTeamEvents(ctx context.Context, teamID uuid.UUID, limit, offset int) ([]store.TeamTaskEventData, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	tid := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.task_id, e.event_type, e.actor_type, e.actor_id, e.data, e.created_at
		 FROM team_task_events e
		 JOIN team_tasks t ON t.id = e.task_id
		 WHERE t.team_id = ? AND t.tenant_id = ?
		 ORDER BY e.created_at DESC
		 LIMIT ? OFFSET ?`,
		teamID, tid, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskEventRows(rows)
}

func scanTaskEventRows(rows *sql.Rows) ([]store.TeamTaskEventData, error) {
	var events []store.TeamTaskEventData
	for rows.Next() {
		var e store.TeamTaskEventData
		var data json.RawMessage
		var createdAt sqliteTime
		if err := rows.Scan(&e.ID, &e.TaskID, &e.EventType, &e.ActorType, &e.ActorID, &data, &createdAt); err != nil {
			return nil, err
		}
		e.CreatedAt = createdAt.Time
		e.Data = data
		events = append(events, e)
	}
	return events, rows.Err()
}

// ============================================================
// Attachments
// ============================================================

func (s *SQLiteTeamStore) AttachFileToTask(ctx context.Context, att *store.TeamTaskAttachmentData) error {
	if att.ID == uuid.Nil {
		att.ID = store.GenNewID()
	}
	att.CreatedAt = time.Now()
	if len(att.Metadata) == 0 {
		att.Metadata = json.RawMessage(`{}`)
	}
	// SQLite has no GENERATED columns via modernc driver — compute app-side.
	if att.BaseName == "" && att.Path != "" {
		att.BaseName = ComputeAttachmentBaseName(att.Path)
	}
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO team_task_attachments (id, task_id, team_id, chat_id, path, base_name, file_size, mime_type, created_by_agent_id, created_by_sender_id, metadata, created_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (task_id, path) DO NOTHING`,
		att.ID, att.TaskID, att.TeamID, att.ChatID, att.Path, att.BaseName,
		att.FileSize, att.MimeType, att.CreatedByAgentID,
		nilStr(att.CreatedBySenderID),
		att.Metadata, att.CreatedAt, tid,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		_, _ = s.db.ExecContext(ctx,
			`UPDATE team_tasks SET attachment_count = attachment_count + 1 WHERE id = ? AND tenant_id = ?`, att.TaskID, tid)
	}
	return nil
}

func (s *SQLiteTeamStore) GetAttachment(ctx context.Context, attachmentID uuid.UUID) (*store.TeamTaskAttachmentData, error) {
	var a store.TeamTaskAttachmentData
	var agentID *uuid.UUID
	var senderID sql.NullString
	var metadata json.RawMessage
	var createdAt sqliteTime
	tid := tenantIDForInsert(ctx)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, task_id, team_id, chat_id, path, file_size, mime_type,
		        created_by_agent_id, created_by_sender_id, metadata, created_at
		 FROM team_task_attachments WHERE id = ? AND tenant_id = ?`, attachmentID, tid,
	).Scan(&a.ID, &a.TaskID, &a.TeamID, &a.ChatID, &a.Path, &a.FileSize, &a.MimeType,
		&agentID, &senderID, &metadata, &createdAt)
	if err != nil {
		return nil, err
	}
	a.CreatedAt = createdAt.Time
	a.CreatedByAgentID = agentID
	if senderID.Valid {
		a.CreatedBySenderID = senderID.String
	}
	a.Metadata = metadata
	return &a, nil
}

func (s *SQLiteTeamStore) ListTaskAttachments(ctx context.Context, taskID uuid.UUID) ([]store.TeamTaskAttachmentData, error) {
	tid := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id, team_id, chat_id, path, file_size, mime_type,
		        created_by_agent_id, created_by_sender_id, metadata, created_at
		 FROM team_task_attachments
		 WHERE task_id = ? AND tenant_id = ?
		 ORDER BY created_at ASC`, taskID, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var atts []store.TeamTaskAttachmentData
	for rows.Next() {
		var a store.TeamTaskAttachmentData
		var agentID *uuid.UUID
		var senderID sql.NullString
		var metadata json.RawMessage
		var createdAt sqliteTime
		if err := rows.Scan(&a.ID, &a.TaskID, &a.TeamID, &a.ChatID, &a.Path, &a.FileSize, &a.MimeType,
			&agentID, &senderID, &metadata, &createdAt); err != nil {
			return nil, err
		}
		a.CreatedAt = createdAt.Time
		a.CreatedByAgentID = agentID
		if senderID.Valid {
			a.CreatedBySenderID = senderID.String
		}
		a.Metadata = metadata
		atts = append(atts, a)
	}
	return atts, rows.Err()
}

func (s *SQLiteTeamStore) DetachFileFromTask(ctx context.Context, taskID uuid.UUID, path string) error {
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM team_task_attachments WHERE task_id = ? AND path = ? AND tenant_id = ?`,
		taskID, path, tid,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		_, _ = s.db.ExecContext(ctx,
			`UPDATE team_tasks SET attachment_count = MAX(attachment_count - 1, 0) WHERE id = ? AND tenant_id = ?`, taskID, tid)

		// Phase 04: clean up Phase 2.5 auto-links sourced from this task.
		source := "task:" + taskID.String()
		if _, derr := s.db.ExecContext(ctx, `
			DELETE FROM vault_links
			WHERE json_extract(metadata, '$.source') = ?
			  AND (
			    from_doc_id IN (SELECT id FROM vault_documents WHERE tenant_id = ?)
			    OR to_doc_id IN (SELECT id FROM vault_documents WHERE tenant_id = ?)
			  )
		`, source, tid, tid); derr != nil {
			slog.Warn("vault.link.cleanup_on_detach", "task_id", taskID, "err", derr)
		}
	}
	return nil
}

// ============================================================
// Progress
// ============================================================

func (s *SQLiteTeamStore) UpdateTaskProgress(ctx context.Context, taskID, teamID uuid.UUID, percent int, step string) error {
	if percent < 0 || percent > 100 {
		return fmt.Errorf("progress percent must be 0-100, got %d", percent)
	}
	now := time.Now()
	lockExpires := now.Add(taskLockDuration)
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET progress_percent = ?, progress_step = ?, lock_expires_at = ?, updated_at = ?
		 WHERE id = ? AND status = ? AND team_id = ? AND tenant_id = ?`,
		percent, step, lockExpires, now,
		taskID, store.TeamTaskStatusInProgress, teamID, tid,
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

func (s *SQLiteTeamStore) RenewTaskLock(ctx context.Context, taskID, teamID uuid.UUID) error {
	now := time.Now()
	lockExpires := now.Add(taskLockDuration)
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET lock_expires_at = ?, updated_at = ?
		 WHERE id = ? AND team_id = ? AND status = ? AND tenant_id = ?`,
		lockExpires, now,
		taskID, teamID, store.TeamTaskStatusInProgress, tid,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task not in progress or not found")
	}
	return nil
}

// ============================================================
// Stale recovery
// ============================================================

// v2ActiveTeamJoin is the JOIN that filters to v2 active teams.
// SQLite uses json_extract instead of PG's ->> operator.
const v2ActiveTeamJoin = `JOIN agent_teams tm ON tm.id = t.team_id
		 AND tm.status = 'active'
		 AND COALESCE(CAST(json_extract(tm.settings, '$.version') AS INTEGER), 0) >= 2`

func (s *SQLiteTeamStore) RecoverAllStaleTasks(ctx context.Context) ([]store.RecoveredTaskInfo, error) {
	now := time.Now()
	// SQLite: UPDATE ... FROM not supported in older versions; use subquery in WHERE.
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, locked_at = NULL, lock_expires_at = NULL,
		     followup_at = NULL, followup_count = 0, followup_message = NULL,
		     followup_channel = NULL, followup_chat_id = NULL, updated_at = ?
		 WHERE status = ? AND lock_expires_at IS NOT NULL AND lock_expires_at < ?
		   AND team_id IN (
		     SELECT id FROM agent_teams WHERE status = 'active'
		       AND COALESCE(CAST(json_extract(settings, '$.version') AS INTEGER), 0) >= 2
		   )`,
		store.TeamTaskStatusPending, now, store.TeamTaskStatusInProgress, now,
	)
	if err != nil {
		return nil, err
	}
	return s.queryRecoveredTasks(ctx, store.TeamTaskStatusPending)
}

func (s *SQLiteTeamStore) ForceRecoverAllTasks(ctx context.Context) ([]store.RecoveredTaskInfo, error) {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, locked_at = NULL, lock_expires_at = NULL,
		     followup_at = NULL, followup_count = 0, followup_message = NULL,
		     followup_channel = NULL, followup_chat_id = NULL, updated_at = ?
		 WHERE status = ?
		   AND team_id IN (
		     SELECT id FROM agent_teams WHERE status = 'active'
		       AND COALESCE(CAST(json_extract(settings, '$.version') AS INTEGER), 0) >= 2
		   )`,
		store.TeamTaskStatusPending, now, store.TeamTaskStatusInProgress,
	)
	if err != nil {
		return nil, err
	}
	return s.queryRecoveredTasks(ctx, store.TeamTaskStatusPending)
}

func (s *SQLiteTeamStore) ListRecoverableTasks(ctx context.Context, teamID uuid.UUID) ([]store.TeamTaskData, error) {
	now := time.Now()
	tid := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE t.team_id = ? AND t.tenant_id = ?
		   AND (
		     t.status = ?
		     OR (t.status = ? AND t.lock_expires_at IS NOT NULL AND t.lock_expires_at < ?)
		   )
		 ORDER BY t.priority DESC, t.created_at
		 LIMIT ?`,
		teamID, tid,
		store.TeamTaskStatusPending, store.TeamTaskStatusInProgress, now, maxListTasksRows)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

func (s *SQLiteTeamStore) MarkAllStaleTasks(ctx context.Context, olderThan time.Time) ([]store.RecoveredTaskInfo, error) {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, updated_at = ?
		 WHERE status = ? AND updated_at < ?
		   AND team_id IN (
		     SELECT id FROM agent_teams WHERE status = 'active'
		       AND COALESCE(CAST(json_extract(settings, '$.version') AS INTEGER), 0) >= 2
		   )`,
		store.TeamTaskStatusStale, now, store.TeamTaskStatusPending, olderThan,
	)
	if err != nil {
		return nil, err
	}
	return s.queryRecoveredTasks(ctx, store.TeamTaskStatusStale)
}

func (s *SQLiteTeamStore) MarkInReviewStaleTasks(ctx context.Context, olderThan time.Time) ([]store.RecoveredTaskInfo, error) {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, updated_at = ?
		 WHERE status = ? AND updated_at < ?
		   AND team_id IN (
		     SELECT id FROM agent_teams WHERE status = 'active'
		       AND COALESCE(CAST(json_extract(settings, '$.version') AS INTEGER), 0) >= 2
		   )`,
		store.TeamTaskStatusStale, now, store.TeamTaskStatusInReview, olderThan,
	)
	if err != nil {
		return nil, err
	}
	return s.queryRecoveredTasks(ctx, store.TeamTaskStatusStale)
}

// FixOrphanedBlockedTasks unblocks blocked tasks where all blockers have reached terminal status.
func (s *SQLiteTeamStore) FixOrphanedBlockedTasks(ctx context.Context) ([]store.RecoveredTaskInfo, error) {
	now := time.Now()
	// Find blocked tasks where every entry in blocked_by is in terminal status.
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.team_id, t.tenant_id, t.task_number, t.subject,
		        COALESCE(t.channel, ''), COALESCE(t.chat_id, '')
		 FROM team_tasks t
		 JOIN agent_teams tm ON tm.id = t.team_id AND tm.status = 'active'
		   AND COALESCE(CAST(json_extract(tm.settings, '$.version') AS INTEGER), 0) >= 2
		 WHERE t.status = 'blocked'
		   AND t.blocked_by IS NOT NULL AND t.blocked_by != '[]'
		   AND NOT EXISTS (
		     SELECT 1 FROM json_each(t.blocked_by) AS je
		     JOIN team_tasks bt ON bt.id = je.value AND bt.tenant_id = t.tenant_id
		     WHERE bt.status NOT IN (?, ?, ?)
		   )`,
		store.TeamTaskStatusCompleted, store.TeamTaskStatusFailed, store.TeamTaskStatusCancelled,
	)
	if err != nil {
		return nil, err
	}
	infos, err := scanRecoveredTaskInfoRows(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}

	// Unblock each — clear blocked_by and set pending.
	for _, info := range infos {
		_, _ = s.db.ExecContext(ctx,
			`UPDATE team_tasks SET blocked_by = '[]', status = ?, updated_at = ? WHERE id = ? AND tenant_id = ?`,
			store.TeamTaskStatusPending, now, info.ID, info.TenantID,
		)
	}
	return infos, nil
}

// queryRecoveredTasks fetches recently-updated tasks matching the given status for recovery reporting.
func (s *SQLiteTeamStore) queryRecoveredTasks(ctx context.Context, status string) ([]store.RecoveredTaskInfo, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT t.id, t.team_id, t.tenant_id, t.task_number, t.subject,
		        COALESCE(t.channel, ''), COALESCE(t.chat_id, '')
		 FROM team_tasks t
		 WHERE t.status = ?
		   AND t.updated_at >= ?
		   AND t.team_id IN (
		     SELECT id FROM agent_teams WHERE status = 'active'
		       AND COALESCE(CAST(json_extract(settings, '$.version') AS INTEGER), 0) >= 2
		   )
		 ORDER BY t.updated_at DESC
		 LIMIT 200`,
		status, time.Now().Add(-5*time.Second),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecoveredTaskInfoRows(rows)
}

func scanRecoveredTaskInfoRows(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]store.RecoveredTaskInfo, error) {
	var out []store.RecoveredTaskInfo
	for rows.Next() {
		var info store.RecoveredTaskInfo
		if err := rows.Scan(&info.ID, &info.TeamID, &info.TenantID, &info.TaskNumber, &info.Subject, &info.Channel, &info.ChatID); err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *SQLiteTeamStore) ResetTaskStatus(ctx context.Context, taskID, teamID uuid.UUID) error {
	now := time.Now()
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, locked_at = NULL, lock_expires_at = NULL, result = NULL,
		 progress_percent = NULL, progress_step = NULL, updated_at = ?
		 WHERE id = ? AND team_id = ? AND status IN (?, ?, ?, ?) AND tenant_id = ?`,
		store.TeamTaskStatusPending, now,
		taskID, teamID,
		store.TeamTaskStatusStale, store.TeamTaskStatusFailed,
		store.TeamTaskStatusCancelled, store.TeamTaskStatusInReview, tid,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("task not available for reset (not stale/failed/cancelled/in_review or wrong team)")
	}
	return nil
}

// ============================================================
// Follow-up reminders
// ============================================================

func (s *SQLiteTeamStore) SetTaskFollowup(ctx context.Context, taskID, teamID uuid.UUID, followupAt time.Time, max int, message, channel, chatID string) error {
	now := time.Now()
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET followup_at = ?, followup_max = ?, followup_message = ?, followup_channel = ?, followup_chat_id = ?, updated_at = ?
		 WHERE id = ? AND team_id = ? AND status = ? AND tenant_id = ?`,
		followupAt, max, message, channel, chatID, now,
		taskID, teamID, store.TeamTaskStatusInProgress, tid,
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

func (s *SQLiteTeamStore) ClearTaskFollowup(ctx context.Context, taskID uuid.UUID) error {
	tid := tenantIDForInsert(ctx)
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET followup_at = NULL, followup_count = 0, followup_message = NULL, followup_channel = NULL, followup_chat_id = NULL, updated_at = ?
		 WHERE id = ? AND tenant_id = ?`,
		time.Now(), taskID, tid,
	)
	return err
}

func (s *SQLiteTeamStore) ListAllFollowupDueTasks(ctx context.Context) ([]store.TeamTaskData, error) {
	now := time.Now()
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 `+v2ActiveTeamJoin+`
		 WHERE t.followup_at IS NOT NULL
		   AND t.followup_at <= ?
		   AND t.status = ?
		   AND (t.followup_max = 0 OR t.followup_count < t.followup_max)
		 ORDER BY t.followup_at
		 LIMIT 100`,
		now, store.TeamTaskStatusInProgress,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

func (s *SQLiteTeamStore) IncrementFollowupCount(ctx context.Context, taskID uuid.UUID, nextAt *time.Time) error {
	tid := tenantIDForInsert(ctx)
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET followup_count = followup_count + 1, followup_at = ?, updated_at = ?
		 WHERE id = ? AND tenant_id = ?`,
		nextAt, time.Now(), taskID, tid,
	)
	return err
}

func (s *SQLiteTeamStore) ClearFollowupByScope(ctx context.Context, channel, chatID string) (int, error) {
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks
		 SET followup_at = NULL, followup_count = 0, followup_message = NULL,
		     followup_channel = NULL, followup_chat_id = NULL, updated_at = ?
		 WHERE followup_channel = ? AND followup_chat_id = ?
		   AND followup_at IS NOT NULL AND status = ? AND tenant_id = ?`,
		time.Now(), channel, chatID, store.TeamTaskStatusInProgress, tid,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (s *SQLiteTeamStore) SetFollowupForActiveTasks(ctx context.Context, teamID uuid.UUID, channel, chatID string, followupAt time.Time, max int, message string) (int, error) {
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks
		 SET followup_at = ?, followup_max = ?, followup_message = ?,
		     followup_channel = ?, followup_chat_id = ?, updated_at = ?
		 WHERE team_id = ?
		   AND status = ?
		   AND followup_at IS NULL
		   AND tenant_id = ?
		   AND (
		     (COALESCE(channel,'') = ? AND COALESCE(chat_id,'') = ?)
		     OR followup_channel = ?
		     OR (COALESCE(channel,'') IN ('', 'system', 'delegate') AND COALESCE(chat_id,'') = '')
		   )`,
		followupAt, max, message, channel, chatID, time.Now(),
		teamID, store.TeamTaskStatusInProgress, tid,
		channel, chatID, channel,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (s *SQLiteTeamStore) HasActiveMemberTasks(ctx context.Context, teamID uuid.UUID, excludeAgentID uuid.UUID) (bool, error) {
	tid := tenantIDForInsert(ctx)
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM team_tasks
			WHERE team_id = ?
			  AND owner_agent_id IS NOT NULL
			  AND owner_agent_id != ?
			  AND status IN (?, ?, ?)
			  AND tenant_id = ?
		)`,
		teamID, excludeAgentID,
		store.TeamTaskStatusPending, store.TeamTaskStatusInProgress, store.TeamTaskStatusBlocked, tid,
	).Scan(&exists)
	return exists, err
}

// BackfillTaskEmbeddings is a no-op for SQLite — vector search not supported.
func (s *SQLiteTeamStore) BackfillTaskEmbeddings(_ context.Context) (int, error) {
	return 0, nil
}

// SearchTasksByEmbedding is a no-op for SQLite — falls back to LIKE search.
func (s *SQLiteTeamStore) SearchTasksByEmbedding(_ context.Context, _ uuid.UUID, _ []float32, _ int, _ string) ([]store.TeamTaskData, error) {
	return nil, nil
}

package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// ============================================================
// Task comments
// ============================================================

func (s *PGTeamStore) AddTaskComment(ctx context.Context, comment *store.TeamTaskCommentData) error {
	if comment.ID == uuid.Nil {
		comment.ID = store.GenNewID()
	}
	comment.CreatedAt = time.Now()
	commentType := comment.CommentType
	if commentType == "" {
		commentType = "note"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_task_comments (id, task_id, agent_id, user_id, content, comment_type, created_at, tenant_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		comment.ID, comment.TaskID, comment.AgentID,
		sql.NullString{String: comment.UserID, Valid: comment.UserID != ""},
		comment.Content, commentType, comment.CreatedAt, tenantIDForInsert(ctx),
	)
	if err != nil {
		return err
	}
	// Increment denormalized comment count.
	_, _ = s.db.ExecContext(ctx,
		`UPDATE team_tasks SET comment_count = comment_count + 1 WHERE id = $1 AND tenant_id = $2`, comment.TaskID, tenantIDForInsert(ctx))
	return nil
}

func (s *PGTeamStore) ListTaskComments(ctx context.Context, taskID uuid.UUID) ([]store.TeamTaskCommentData, error) {
	tid := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.task_id, c.agent_id, c.user_id, c.content, c.comment_type, c.created_at,
		 COALESCE(a.agent_key, '') AS agent_key
		 FROM team_task_comments c
		 LEFT JOIN agents a ON a.id = c.agent_id
		 WHERE c.task_id = $1 AND c.tenant_id = $2
		 ORDER BY c.created_at ASC`, taskID, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []store.TeamTaskCommentData
	for rows.Next() {
		var c store.TeamTaskCommentData
		var agentID *uuid.UUID
		var userID sql.NullString
		if err := rows.Scan(&c.ID, &c.TaskID, &agentID, &userID, &c.Content, &c.CommentType, &c.CreatedAt, &c.AgentKey); err != nil {
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

// ListRecentTaskComments returns the N most recent comments for a task (DESC order).
// Used by dispatch to include context without fetching all comments.
func (s *PGTeamStore) ListRecentTaskComments(ctx context.Context, taskID uuid.UUID, limit int) ([]store.TeamTaskCommentData, error) {
	tid := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.task_id, c.agent_id, c.user_id, c.content, c.comment_type, c.created_at,
		 COALESCE(a.agent_key, '') AS agent_key
		 FROM team_task_comments c
		 LEFT JOIN agents a ON a.id = c.agent_id
		 WHERE c.task_id = $1 AND c.tenant_id = $3
		 ORDER BY c.created_at DESC
		 LIMIT $2`, taskID, limit, tid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []store.TeamTaskCommentData
	for rows.Next() {
		var c store.TeamTaskCommentData
		var agentID *uuid.UUID
		var userID sql.NullString
		if err := rows.Scan(&c.ID, &c.TaskID, &agentID, &userID, &c.Content, &c.CommentType, &c.CreatedAt, &c.AgentKey); err != nil {
			return nil, err
		}
		c.AgentID = agentID
		if userID.Valid {
			c.UserID = userID.String
		}
		comments = append(comments, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse to chronological order (ASC) for display.
	for i, j := 0, len(comments)-1; i < j; i, j = i+1, j-1 {
		comments[i], comments[j] = comments[j], comments[i]
	}
	return comments, nil
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
		`INSERT INTO team_task_events (id, task_id, event_type, actor_type, actor_id, data, created_at, tenant_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		event.ID, event.TaskID, event.EventType, event.ActorType, event.ActorID, event.Data, event.CreatedAt, tenantIDForInsert(ctx),
	)
	return err
}

func (s *PGTeamStore) ListTaskEvents(ctx context.Context, taskID uuid.UUID) ([]store.TeamTaskEventData, error) {
	tid := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id, event_type, actor_type, actor_id, data, created_at
		 FROM team_task_events
		 WHERE task_id = $1 AND tenant_id = $2
		 ORDER BY created_at ASC`, taskID, tid)
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

func (s *PGTeamStore) ListTeamEvents(ctx context.Context, teamID uuid.UUID, limit, offset int) ([]store.TeamTaskEventData, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	tid := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.task_id, e.event_type, e.actor_type, e.actor_id, e.data, e.created_at
		 FROM team_task_events e
		 JOIN team_tasks t ON t.id = e.task_id
		 WHERE t.team_id = $1 AND t.tenant_id = $4
		 ORDER BY e.created_at DESC
		 LIMIT $2 OFFSET $3`,
		teamID, limit, offset, tid)
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
// Attachments (path-based, no FK to workspace files)
// ============================================================

func (s *PGTeamStore) AttachFileToTask(ctx context.Context, att *store.TeamTaskAttachmentData) error {
	if att.ID == uuid.Nil {
		att.ID = store.GenNewID()
	}
	att.CreatedAt = time.Now()
	if len(att.Metadata) == 0 {
		att.Metadata = json.RawMessage(`{}`)
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO team_task_attachments (id, task_id, team_id, chat_id, path, file_size, mime_type, created_by_agent_id, created_by_sender_id, metadata, created_at, tenant_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 ON CONFLICT (task_id, path) DO NOTHING`,
		att.ID, att.TaskID, att.TeamID, att.ChatID, att.Path,
		att.FileSize, att.MimeType, att.CreatedByAgentID,
		sql.NullString{String: att.CreatedBySenderID, Valid: att.CreatedBySenderID != ""},
		att.Metadata, att.CreatedAt, tenantIDForInsert(ctx),
	)
	if err != nil {
		return err
	}
	// Increment denormalized count only if a row was actually inserted (not conflict).
	if n, _ := res.RowsAffected(); n > 0 {
		_, _ = s.db.ExecContext(ctx,
			`UPDATE team_tasks SET attachment_count = attachment_count + 1 WHERE id = $1 AND tenant_id = $2`, att.TaskID, tenantIDForInsert(ctx))
	}
	return nil
}

func (s *PGTeamStore) GetAttachment(ctx context.Context, attachmentID uuid.UUID) (*store.TeamTaskAttachmentData, error) {
	var a store.TeamTaskAttachmentData
	var agentID *uuid.UUID
	var senderID sql.NullString
	var metadata json.RawMessage
	tid := tenantIDForInsert(ctx)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, task_id, team_id, chat_id, path, file_size, mime_type,
		        created_by_agent_id, created_by_sender_id, metadata, created_at
		 FROM team_task_attachments WHERE id = $1 AND tenant_id = $2`, attachmentID, tid,
	).Scan(&a.ID, &a.TaskID, &a.TeamID, &a.ChatID, &a.Path, &a.FileSize, &a.MimeType,
		&agentID, &senderID, &metadata, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	a.CreatedByAgentID = agentID
	if senderID.Valid {
		a.CreatedBySenderID = senderID.String
	}
	a.Metadata = metadata
	return &a, nil
}

func (s *PGTeamStore) ListTaskAttachments(ctx context.Context, taskID uuid.UUID) ([]store.TeamTaskAttachmentData, error) {
	tid := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id, team_id, chat_id, path, file_size, mime_type,
		        created_by_agent_id, created_by_sender_id, metadata, created_at
		 FROM team_task_attachments
		 WHERE task_id = $1 AND tenant_id = $2
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
		if err := rows.Scan(&a.ID, &a.TaskID, &a.TeamID, &a.ChatID, &a.Path, &a.FileSize, &a.MimeType,
			&agentID, &senderID, &metadata, &a.CreatedAt); err != nil {
			return nil, err
		}
		a.CreatedByAgentID = agentID
		if senderID.Valid {
			a.CreatedBySenderID = senderID.String
		}
		a.Metadata = metadata
		atts = append(atts, a)
	}
	return atts, rows.Err()
}

func (s *PGTeamStore) DetachFileFromTask(ctx context.Context, taskID uuid.UUID, path string) error {
	tid := tenantIDForInsert(ctx)
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM team_task_attachments WHERE task_id = $1 AND path = $2 AND tenant_id = $3`,
		taskID, path, tid,
	)
	if err != nil {
		return err
	}
	// Decrement denormalized count only if a row was actually deleted.
	if n, _ := res.RowsAffected(); n > 0 {
		_, _ = s.db.ExecContext(ctx,
			`UPDATE team_tasks SET attachment_count = GREATEST(attachment_count - 1, 0) WHERE id = $1 AND tenant_id = $2`, taskID, tid)
	}
	return nil
}

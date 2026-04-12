package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
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

// taskCommentRow is a scan struct for team_task_comments with nullable user_id.
type taskCommentRow struct {
	ID          uuid.UUID  `json:"id" db:"id"`
	TaskID      uuid.UUID  `json:"task_id" db:"task_id"`
	AgentID     *uuid.UUID `json:"agent_id" db:"agent_id"`
	UserID      *string    `json:"user_id" db:"user_id"`
	Content     string     `json:"content" db:"content"`
	CommentType string     `json:"comment_type" db:"comment_type"`
	CreatedAt   time.Time  `json:"created_at" db:"created_at"`
	AgentKey    string     `json:"agent_key" db:"agent_key"`
}

func (r taskCommentRow) toCommentData() store.TeamTaskCommentData {
	c := store.TeamTaskCommentData{
		ID: r.ID, TaskID: r.TaskID, AgentID: r.AgentID,
		Content: r.Content, CommentType: r.CommentType, CreatedAt: r.CreatedAt, AgentKey: r.AgentKey,
	}
	if r.UserID != nil {
		c.UserID = *r.UserID
	}
	return c
}

func (s *PGTeamStore) ListTaskComments(ctx context.Context, taskID uuid.UUID) ([]store.TeamTaskCommentData, error) {
	tid := tenantIDForInsert(ctx)
	var rows []taskCommentRow
	err := pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT c.id, c.task_id, c.agent_id, c.user_id, c.content, c.comment_type, c.created_at,
		 COALESCE(a.agent_key, '') AS agent_key
		 FROM team_task_comments c
		 LEFT JOIN agents a ON a.id = c.agent_id
		 WHERE c.task_id = $1 AND c.tenant_id = $2
		 ORDER BY c.created_at ASC`, taskID, tid)
	if err != nil {
		return nil, err
	}
	comments := make([]store.TeamTaskCommentData, len(rows))
	for i, r := range rows {
		comments[i] = r.toCommentData()
	}
	return comments, nil
}

// ListRecentTaskComments returns the N most recent comments for a task (DESC order).
// Used by dispatch to include context without fetching all comments.
func (s *PGTeamStore) ListRecentTaskComments(ctx context.Context, taskID uuid.UUID, limit int) ([]store.TeamTaskCommentData, error) {
	tid := tenantIDForInsert(ctx)
	var rows []taskCommentRow
	err := pkgSqlxDB.SelectContext(ctx, &rows,
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
	comments := make([]store.TeamTaskCommentData, len(rows))
	for i, r := range rows {
		comments[i] = r.toCommentData()
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
	var events []store.TeamTaskEventData
	err := pkgSqlxDB.SelectContext(ctx, &events,
		`SELECT id, task_id, event_type, actor_type, actor_id, COALESCE(data, '{}') AS data, created_at
		 FROM team_task_events
		 WHERE task_id = $1 AND tenant_id = $2
		 ORDER BY created_at ASC`, taskID, tid)
	return events, err
}

func (s *PGTeamStore) ListTeamEvents(ctx context.Context, teamID uuid.UUID, limit, offset int) ([]store.TeamTaskEventData, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	tid := tenantIDForInsert(ctx)
	var events []store.TeamTaskEventData
	err := pkgSqlxDB.SelectContext(ctx, &events,
		`SELECT e.id, e.task_id, e.event_type, e.actor_type, e.actor_id, COALESCE(e.data, '{}') AS data, e.created_at
		 FROM team_task_events e
		 JOIN team_tasks t ON t.id = e.task_id
		 WHERE t.team_id = $1 AND t.tenant_id = $4
		 ORDER BY e.created_at DESC
		 LIMIT $2 OFFSET $3`,
		teamID, limit, offset, tid)
	return events, err
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

// taskAttachmentRow is a scan struct for team_task_attachments with nullable created_by_sender_id.
type taskAttachmentRow struct {
	ID                uuid.UUID       `json:"id" db:"id"`
	TaskID            uuid.UUID       `json:"task_id" db:"task_id"`
	TeamID            uuid.UUID       `json:"team_id" db:"team_id"`
	ChatID            string          `json:"chat_id" db:"chat_id"`
	Path              string          `json:"path" db:"path"`
	FileSize          int64           `json:"file_size" db:"file_size"`
	MimeType          string          `json:"mime_type" db:"mime_type"`
	CreatedByAgentID  *uuid.UUID      `json:"created_by_agent_id" db:"created_by_agent_id"`
	CreatedBySenderID *string         `json:"created_by_sender_id" db:"created_by_sender_id"`
	Metadata          json.RawMessage `json:"metadata" db:"metadata"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
}

func (r taskAttachmentRow) toAttachmentData() store.TeamTaskAttachmentData {
	a := store.TeamTaskAttachmentData{
		ID: r.ID, TaskID: r.TaskID, TeamID: r.TeamID, ChatID: r.ChatID,
		Path: r.Path, FileSize: r.FileSize, MimeType: r.MimeType,
		CreatedByAgentID: r.CreatedByAgentID, Metadata: r.Metadata, CreatedAt: r.CreatedAt,
	}
	if r.CreatedBySenderID != nil {
		a.CreatedBySenderID = *r.CreatedBySenderID
	}
	return a
}

func (s *PGTeamStore) ListTaskAttachments(ctx context.Context, taskID uuid.UUID) ([]store.TeamTaskAttachmentData, error) {
	tid := tenantIDForInsert(ctx)
	var rows []taskAttachmentRow
	err := pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT id, task_id, team_id, COALESCE(chat_id,'') AS chat_id, path, file_size, COALESCE(mime_type,'') AS mime_type,
		        created_by_agent_id, created_by_sender_id, COALESCE(metadata,'{}') AS metadata, created_at
		 FROM team_task_attachments
		 WHERE task_id = $1 AND tenant_id = $2
		 ORDER BY created_at ASC`, taskID, tid)
	if err != nil {
		return nil, err
	}
	atts := make([]store.TeamTaskAttachmentData, len(rows))
	for i, r := range rows {
		atts[i] = r.toAttachmentData()
	}
	return atts, nil
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

		// Phase 04: clean up Phase 2.5 auto-links sourced from this task.
		// Scoped DELETE via source key — broader than the single detached
		// basename, but the task's auto-link group is meaningful as a whole.
		source := "task:" + taskID.String()
		if delRes, derr := s.db.ExecContext(ctx, `
			DELETE FROM vault_links vl
			USING vault_documents vd
			WHERE vl.metadata->>'source' = $1
			  AND vd.tenant_id = $2
			  AND (vl.from_doc_id = vd.id OR vl.to_doc_id = vd.id)
		`, source, tid); derr != nil {
			slog.Warn("vault.link.cleanup_on_detach", "task_id", taskID, "err", derr)
		} else if cnt, _ := delRes.RowsAffected(); cnt > 0 {
			slog.Info("vault.link.deleted_on_detach",
				"task_id", taskID, "count", cnt)
		}
	}
	return nil
}

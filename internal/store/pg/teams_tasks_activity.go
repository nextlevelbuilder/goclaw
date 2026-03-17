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

func (s *PGTeamStore) ListTeamEvents(ctx context.Context, teamID uuid.UUID, limit, offset int) ([]store.TeamTaskEventData, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id, e.task_id, e.event_type, e.actor_type, e.actor_id, e.data, e.created_at
		 FROM team_task_events e
		 JOIN team_tasks t ON t.id = e.task_id
		 WHERE t.team_id = $1
		 ORDER BY e.created_at DESC
		 LIMIT $2 OFFSET $3`,
		teamID, limit, offset)
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

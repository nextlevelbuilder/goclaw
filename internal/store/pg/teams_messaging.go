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
// Messages
// ============================================================

func (s *PGTeamStore) SendMessage(ctx context.Context, msg *store.TeamMessageData) error {
	if msg.ID == uuid.Nil {
		msg.ID = store.GenNewID()
	}
	msg.CreatedAt = time.Now()

	metadata, _ := json.Marshal(msg.Metadata)
	if len(metadata) == 0 {
		metadata = []byte(`{}`)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_messages (id, team_id, from_agent_id, to_agent_id, content, message_type, read, task_id, metadata, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		msg.ID, msg.TeamID, msg.FromAgentID, msg.ToAgentID,
		msg.Content, msg.MessageType, false, msg.TaskID, metadata, msg.CreatedAt,
	)
	return err
}

func (s *PGTeamStore) GetUnread(ctx context.Context, teamID, agentID uuid.UUID) ([]store.TeamMessageData, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT m.id, m.team_id, m.from_agent_id, m.to_agent_id, m.content, m.message_type, m.read, m.task_id, m.metadata, m.created_at,
		 COALESCE(fa.agent_key, '') AS from_agent_key,
		 COALESCE(ta.agent_key, '') AS to_agent_key
		 FROM team_messages m
		 LEFT JOIN agents fa ON fa.id = m.from_agent_id
		 LEFT JOIN agents ta ON ta.id = m.to_agent_id
		 WHERE m.team_id = $1 AND (m.to_agent_id = $2 OR m.to_agent_id IS NULL) AND m.read = false
		 ORDER BY m.created_at
		 LIMIT 50`, teamID, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMessageRowsJoined(rows)
}

func (s *PGTeamStore) MarkRead(ctx context.Context, messageID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_messages SET read = true WHERE id = $1`, messageID)
	return err
}

func (s *PGTeamStore) ListMessages(ctx context.Context, teamID uuid.UUID, limit, offset int) ([]store.TeamMessageData, int, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	// Count total
	var total int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM team_messages WHERE team_id = $1`, teamID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT m.id, m.team_id, m.from_agent_id, m.to_agent_id, m.content, m.message_type, m.read, m.task_id, m.metadata, m.created_at,
		 COALESCE(fa.agent_key, '') AS from_agent_key,
		 COALESCE(ta.agent_key, '') AS to_agent_key
		 FROM team_messages m
		 LEFT JOIN agents fa ON fa.id = m.from_agent_id
		 LEFT JOIN agents ta ON ta.id = m.to_agent_id
		 WHERE m.team_id = $1
		 ORDER BY m.created_at DESC
		 LIMIT $2 OFFSET $3`, teamID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	messages, err := scanMessageRowsJoined(rows)
	if err != nil {
		return nil, 0, err
	}
	return messages, total, nil
}

func scanMessageRowsJoined(rows *sql.Rows) ([]store.TeamMessageData, error) {
	var messages []store.TeamMessageData
	for rows.Next() {
		var d store.TeamMessageData
		var toAgentID, taskID *uuid.UUID
		var metadata json.RawMessage
		if err := rows.Scan(
			&d.ID, &d.TeamID, &d.FromAgentID, &toAgentID,
			&d.Content, &d.MessageType, &d.Read, &taskID, &metadata, &d.CreatedAt,
			&d.FromAgentKey, &d.ToAgentKey,
		); err != nil {
			return nil, err
		}
		d.ToAgentID = toAgentID
		d.TaskID = taskID
		if len(metadata) > 0 && string(metadata) != "{}" {
			_ = json.Unmarshal(metadata, &d.Metadata)
		}
		messages = append(messages, d)
	}
	return messages, rows.Err()
}


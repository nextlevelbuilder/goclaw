//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteListenRawMessageStore implements store.ListenRawMessageStore backed by SQLite.
type SQLiteListenRawMessageStore struct {
	db *sql.DB
}

// NewSQLiteListenRawMessageStore creates a new SQLiteListenRawMessageStore.
func NewSQLiteListenRawMessageStore(db *sql.DB) *SQLiteListenRawMessageStore {
	return &SQLiteListenRawMessageStore{db: db}
}

func (s *SQLiteListenRawMessageStore) AppendBatch(ctx context.Context, msgs []store.ListenRawMessage) error {
	if len(msgs) == 0 {
		return nil
	}

	const cols = 12
	placeholders := make([]string, len(msgs))
	args := make([]any, 0, len(msgs)*cols)
	now := time.Now()
	tid := tenantIDForInsert(ctx)

	for i := range msgs {
		if msgs[i].ID == uuid.Nil {
			msgs[i].ID = uuid.Must(uuid.NewV7())
		}
		placeholders[i] = "(?,?,?,?,?,?,?,?,?,?,?,?)"
		args = append(args, msgs[i].ID, msgs[i].ChannelName, msgs[i].ChatID,
			msgs[i].ChatName, msgs[i].GraphID, msgs[i].Sender, msgs[i].SenderID,
			msgs[i].Body, msgs[i].MsgTimestamp, msgs[i].AgentID, now, tid)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO listen_raw_messages (id, channel_name, chat_id, chat_name, graph_id, sender, sender_id, body, msg_timestamp, agent_id, created_at, tenant_id)
		 VALUES `+strings.Join(placeholders, ","),
		args...,
	)
	return err
}

func (s *SQLiteListenRawMessageStore) ListPending(ctx context.Context, agentID, graphID string, maxRows int) ([]store.ListenRawMessage, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	args := append([]any{agentID, graphID, maxRows}, tArgs...)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_name, chat_id, chat_name, graph_id, sender, sender_id, body, msg_timestamp, agent_id, created_at, processed_at
		 FROM listen_raw_messages
		 WHERE agent_id = ? AND graph_id = ? AND processed_at IS NULL`+tClause+`
		 ORDER BY msg_timestamp DESC
		 LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.ListenRawMessage
	for rows.Next() {
		var m store.ListenRawMessage
		var processedAt sql.NullString
		var createdAt sql.NullString
		var msgTimestamp sql.NullString
		if err := rows.Scan(&m.ID, &m.ChannelName, &m.ChatID, &m.ChatName,
			&m.GraphID, &m.Sender, &m.SenderID, &m.Body,
			&msgTimestamp, &m.AgentID, &createdAt, &processedAt); err != nil {
			return nil, err
		}
		if msgTimestamp.Valid {
			t, _ := time.Parse(time.RFC3339Nano, msgTimestamp.String)
			m.MsgTimestamp = t
		}
		if createdAt.Valid {
			t, _ := time.Parse(time.RFC3339Nano, createdAt.String)
			m.CreatedAt = t
		}
		if processedAt.Valid && processedAt.String != "" {
			t, _ := time.Parse(time.RFC3339Nano, processedAt.String)
			m.ProcessedAt = &t
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (s *SQLiteListenRawMessageStore) MarkProcessed(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, now)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE listen_raw_messages SET processed_at = ? WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	return err
}

func (s *SQLiteListenRawMessageStore) ListPendingGroups(ctx context.Context) ([]store.ListenRawMessageGroup, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT agent_id, graph_id FROM listen_raw_messages WHERE processed_at IS NULL`+tClause,
		tArgs...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.ListenRawMessageGroup
	for rows.Next() {
		var g store.ListenRawMessageGroup
		if err := rows.Scan(&g.AgentID, &g.GraphID); err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

func (s *SQLiteListenRawMessageStore) ResetProcessed(ctx context.Context, agentID, graphID string) (int64, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return 0, err
	}

	q := `UPDATE listen_raw_messages SET processed_at = NULL WHERE agent_id = ? AND graph_id = ? AND processed_at IS NOT NULL` + tClause
	args := append([]any{agentID, graphID}, tArgs...)

	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *SQLiteListenRawMessageStore) List(ctx context.Context, opts store.ListenRawMessageListOpts) ([]store.ListenRawMessage, int, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, 0, err
	}

	var conditions []string
	var args []any

	if opts.ChannelName != "" {
		conditions = append(conditions, "channel_name = ?")
		args = append(args, opts.ChannelName)
	}
	if opts.ChatID != "" {
		conditions = append(conditions, "chat_id = ?")
		args = append(args, opts.ChatID)
	}
	if opts.AgentID != "" {
		conditions = append(conditions, "agent_id = ?")
		args = append(args, opts.AgentID)
	}
	if opts.Processed != nil {
		if *opts.Processed {
			conditions = append(conditions, "processed_at IS NOT NULL")
		} else {
			conditions = append(conditions, "processed_at IS NULL")
		}
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " AND " + strings.Join(conditions, " AND ")
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	// Count total.
	var total int
	countArgs := append(tArgs, args...)
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM listen_raw_messages WHERE 1=1`+tClause+whereClause,
		countArgs...,
	).Scan(&total)
	if err != nil {
		return nil, 0, err
	}

	// Fetch page.
	offset := opts.Offset
	if offset < 0 {
		offset = 0
	}
	pageArgs := append(tArgs, args...)
	pageArgs = append(pageArgs, limit, offset)

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_name, chat_id, chat_name, graph_id, sender, sender_id, body, msg_timestamp, agent_id, created_at, processed_at
		 FROM listen_raw_messages WHERE 1=1`+tClause+whereClause+`
		 ORDER BY created_at DESC
		 LIMIT ? OFFSET ?`,
		pageArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var result []store.ListenRawMessage
	for rows.Next() {
		var m store.ListenRawMessage
		var processedAt sql.NullString
		var createdAt sql.NullString
		var msgTimestamp sql.NullString
		if err := rows.Scan(&m.ID, &m.ChannelName, &m.ChatID, &m.ChatName,
			&m.GraphID, &m.Sender, &m.SenderID, &m.Body,
			&msgTimestamp, &m.AgentID, &createdAt, &processedAt); err != nil {
			return nil, 0, err
		}
		if msgTimestamp.Valid {
			t, _ := time.Parse(time.RFC3339Nano, msgTimestamp.String)
			m.MsgTimestamp = t
		}
		if createdAt.Valid {
			t, _ := time.Parse(time.RFC3339Nano, createdAt.String)
			m.CreatedAt = t
		}
		if processedAt.Valid && processedAt.String != "" {
			t, _ := time.Parse(time.RFC3339Nano, processedAt.String)
			m.ProcessedAt = &t
		}
		result = append(result, m)
	}
	return result, total, rows.Err()
}

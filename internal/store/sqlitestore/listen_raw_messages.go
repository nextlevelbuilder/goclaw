//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
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

	const cols = 13
	placeholders := make([]string, len(msgs))
	args := make([]any, 0, len(msgs)*cols)
	now := time.Now()
	tid := tenantIDForInsert(ctx)

	for i := range msgs {
		if msgs[i].ID == uuid.Nil {
			msgs[i].ID = uuid.Must(uuid.NewV7())
		}
		mediaJSON, _ := json.Marshal(msgs[i].MediaRefs)
		placeholders[i] = "(?,?,?,?,?,?,?,?,?,?,?,?,?)"
		args = append(args, msgs[i].ID, msgs[i].ChannelName, msgs[i].ChatID,
			msgs[i].ChatName, msgs[i].GraphID, msgs[i].Sender, msgs[i].SenderID,
			msgs[i].Body, msgs[i].MsgTimestamp, msgs[i].AgentID, now, tid, string(mediaJSON))
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO listen_raw_messages (id, channel_name, chat_id, chat_name, graph_id, sender, sender_id, body, msg_timestamp, agent_id, created_at, tenant_id, media_refs)
					 VALUES `+strings.Join(placeholders, ","),
		args...,
	)
	return err
}

func scanRawMessages(rows *sql.Rows) ([]store.ListenRawMessage, error) {
	var result []store.ListenRawMessage
	for rows.Next() {
		var m store.ListenRawMessage
		var processedAt sql.NullString
		var createdAt sql.NullString
		var msgTimestamp sql.NullString
		var mediaRefsJSON string
		var extractionStatus sql.NullString
		var extractionError sql.NullString
		var extractionAttempts sql.NullInt64
		var lastAttemptedAt sql.NullString
		if err := rows.Scan(&m.ID, &m.ChannelName, &m.ChatID, &m.ChatName,
			&m.GraphID, &m.Sender, &m.SenderID, &m.Body,
			&msgTimestamp, &m.AgentID, &createdAt, &processedAt, &mediaRefsJSON,
			&extractionStatus, &extractionError, &extractionAttempts, &lastAttemptedAt); err != nil {
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
		if mediaRefsJSON != "" && mediaRefsJSON != "[]" {
			_ = json.Unmarshal([]byte(mediaRefsJSON), &m.MediaRefs)
		}
		if extractionStatus.Valid {
			m.ExtractionStatus = extractionStatus.String
		}
		if extractionError.Valid {
			m.ExtractionError = extractionError.String
		}
		if extractionAttempts.Valid {
			m.ExtractionAttempts = int(extractionAttempts.Int64)
		}
		if lastAttemptedAt.Valid && lastAttemptedAt.String != "" {
			t, _ := time.Parse(time.RFC3339Nano, lastAttemptedAt.String)
			m.LastAttemptedAt = &t
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

func (s *SQLiteListenRawMessageStore) ListPending(ctx context.Context, agentID, graphID string, maxRows int) ([]store.ListenRawMessage, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	args := append([]any{agentID, graphID,
		store.ExtractionStatusPending, store.ExtractionStatusFailed, store.MaxExtractionAttempts,
		maxRows}, tArgs...)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_name, chat_id, chat_name, graph_id, sender, sender_id, body, msg_timestamp, agent_id, created_at, processed_at, media_refs,
		        extraction_status, extraction_error, extraction_attempts, last_attempted_at
		 FROM listen_raw_messages
		 WHERE agent_id = ? AND graph_id = ?
		   AND (extraction_status = ? OR (extraction_status = ? AND extraction_attempts < ?))`+tClause+`
		 ORDER BY msg_timestamp DESC
		 LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRawMessages(rows)
}

func (s *SQLiteListenRawMessageStore) MarkProcessed(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	args = append(args, now, store.ExtractionStatusExtracted)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE listen_raw_messages SET processed_at = ?, extraction_status = ?, extraction_error = NULL WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	return err
}

func (s *SQLiteListenRawMessageStore) MarkFailed(ctx context.Context, ids []uuid.UUID, extractErr string) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+3)
	args = append(args, store.ExtractionStatusFailed, extractErr, now)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE listen_raw_messages
		 SET extraction_status = ?, extraction_error = ?,
		     extraction_attempts = extraction_attempts + 1,
		     last_attempted_at = ?, processed_at = ?
		 WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	return err
}

func (s *SQLiteListenRawMessageStore) ListPendingGroups(ctx context.Context) ([]store.ListenRawMessageGroup, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	args := append([]any{store.ExtractionStatusPending, store.ExtractionStatusFailed, store.MaxExtractionAttempts}, tArgs...)
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT agent_id, graph_id
		 FROM listen_raw_messages
		 WHERE (extraction_status = ? OR (extraction_status = ? AND extraction_attempts < ?))`+tClause,
		args...,
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

func (s *SQLiteListenRawMessageStore) ListPendingEmbeddingGroups(ctx context.Context) ([]store.ListenRawMessageGroup, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT agent_id, graph_id FROM listen_raw_messages WHERE embedded_at IS NULL`+tClause,
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

func (s *SQLiteListenRawMessageStore) ListPendingEmbeddings(ctx context.Context, agentID, graphID string, maxRows int) ([]store.ListenRawMessage, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_name, chat_id, chat_name, graph_id, sender, sender_id, body, msg_timestamp, agent_id, created_at, processed_at, media_refs,
		        extraction_status, extraction_error, extraction_attempts, last_attempted_at
		 FROM listen_raw_messages
		 WHERE agent_id = ? AND graph_id = ? AND embedded_at IS NULL`+tClause+`
		 ORDER BY msg_timestamp ASC
		 LIMIT ?`,
		append([]any{agentID, graphID, maxRows}, tArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRawMessages(rows)
}

func (s *SQLiteListenRawMessageStore) MarkEmbedded(ctx context.Context, ids []uuid.UUID) error {
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
		`UPDATE listen_raw_messages SET embedded_at = ? WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	return err
}

func (s *SQLiteListenRawMessageStore) ResetProcessed(ctx context.Context, agentID, graphID string) (int64, error) {
	var conditions []string
	var args []any

	if agentID != "" {
		conditions = append(conditions, "agent_id = ?")
		args = append(args, agentID)
	}
	if graphID != "" {
		conditions = append(conditions, "graph_id = ?")
		args = append(args, graphID)
	}
	conditions = append(conditions, "processed_at IS NOT NULL")

	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return 0, err
	}

	where := strings.Join(conditions, " AND ")
	args = append(args, store.ExtractionStatusPending)
	q := `UPDATE listen_raw_messages SET processed_at = NULL, extraction_status = ?, extraction_error = NULL WHERE ` + where + tClause
	args = append(args, tArgs...)

	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *SQLiteListenRawMessageStore) ResetProcessedByIDs(ctx context.Context, ids []uuid.UUID) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return 0, err
	}
	args = append(args, store.ExtractionStatusPending)
	args = append(args, tArgs...)
	q := `UPDATE listen_raw_messages SET processed_at = NULL, extraction_status = ?, extraction_error = NULL WHERE id IN (` + strings.Join(placeholders, ",") + `) AND processed_at IS NOT NULL` + tClause
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
	if opts.GraphID != "" {
		conditions = append(conditions, "graph_id = ?")
		args = append(args, opts.GraphID)
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
		`SELECT id, channel_name, chat_id, chat_name, graph_id, sender, sender_id, body, msg_timestamp, agent_id, created_at, processed_at, media_refs,
		        extraction_status, extraction_error, extraction_attempts, last_attempted_at
		 FROM listen_raw_messages WHERE 1=1`+tClause+whereClause+`
		 ORDER BY created_at DESC
		 LIMIT ? OFFSET ?`,
		pageArgs...,
	)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	result, scanErr := scanRawMessages(rows)
	return result, total, scanErr
}

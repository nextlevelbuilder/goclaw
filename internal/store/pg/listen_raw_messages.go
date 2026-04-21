package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGListenRawMessageStore implements store.ListenRawMessageStore backed by Postgres.
type PGListenRawMessageStore struct {
	db *sql.DB
}

// NewPGListenRawMessageStore creates a new PGListenRawMessageStore.
func NewPGListenRawMessageStore(db *sql.DB) *PGListenRawMessageStore {
	return &PGListenRawMessageStore{db: db}
}

func (s *PGListenRawMessageStore) AppendBatch(ctx context.Context, msgs []store.ListenRawMessage) error {
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
		base := i * cols
		placeholders[i] = fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10, base+11, base+12, base+13)
		args = append(args, msgs[i].ID, msgs[i].ChannelName, msgs[i].ChatID,
			msgs[i].ChatName, msgs[i].GraphID, msgs[i].Sender, msgs[i].SenderID,
			msgs[i].Body, msgs[i].MsgTimestamp, msgs[i].AgentID, now, tid, mediaJSON)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO listen_raw_messages (id, channel_name, chat_id, chat_name, graph_id, sender, sender_id, body, msg_timestamp, agent_id, created_at, tenant_id, media_refs)
				 VALUES `+strings.Join(placeholders, ","),
		args...,
	)
	return err
}

// rawMsgRow is an sqlx scan struct for listen_raw_messages SELECT queries.
// Handles jsonb→json.RawMessage conversion for media_refs.
type rawMsgRow struct {
	ID           uuid.UUID       `db:"id"`
	ChannelName  string          `db:"channel_name"`
	ChatID       string          `db:"chat_id"`
	ChatName     string          `db:"chat_name"`
	GraphID      string          `db:"graph_id"`
	Sender       string          `db:"sender"`
	SenderID     string          `db:"sender_id"`
	Body         string          `db:"body"`
	MsgTimestamp time.Time       `db:"msg_timestamp"`
	AgentID      string          `db:"agent_id"`
	CreatedAt    time.Time       `db:"created_at"`
	ProcessedAt  *time.Time      `db:"processed_at"`
	MediaRefs    json.RawMessage `db:"media_refs"`
	AgentName    string          `db:"agent_name"`
}

func (r rawMsgRow) toMessage() store.ListenRawMessage {
	m := store.ListenRawMessage{
		ID:           r.ID,
		ChannelName:  r.ChannelName,
		ChatID:       r.ChatID,
		ChatName:     r.ChatName,
		GraphID:      r.GraphID,
		Sender:       r.Sender,
		SenderID:     r.SenderID,
		Body:         r.Body,
		MsgTimestamp: r.MsgTimestamp,
		AgentID:      r.AgentID,
		CreatedAt:    r.CreatedAt,
		ProcessedAt:  r.ProcessedAt,
		AgentName:    r.AgentName,
	}
	if len(r.MediaRefs) > 0 && string(r.MediaRefs) != "null" {
		_ = json.Unmarshal(r.MediaRefs, &m.MediaRefs)
	}
	return m
}

func (s *PGListenRawMessageStore) ListPending(ctx context.Context, agentID, graphID string, maxRows int) ([]store.ListenRawMessage, error) {
	tClause, tArgs, _, err := scopeClause(ctx, 4)
	if err != nil {
		return nil, err
	}
	var rows []rawMsgRow
	err = pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT id, channel_name, chat_id, chat_name, graph_id, sender, sender_id, body, msg_timestamp, agent_id, created_at, processed_at, media_refs
				 FROM listen_raw_messages
				 WHERE agent_id = $1 AND graph_id = $2 AND processed_at IS NULL`+tClause+`
				 ORDER BY msg_timestamp DESC
				 LIMIT $3`,
		append([]any{agentID, graphID, maxRows}, tArgs...)...,
	)
	if err != nil {
		return nil, err
	}
	result := make([]store.ListenRawMessage, len(rows))
	for i, r := range rows {
		result[i] = r.toMessage()
	}
	return result, nil
}

func (s *PGListenRawMessageStore) MarkProcessed(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids)+1)
	args[0] = time.Now()
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+2)
		args[i+1] = id
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE listen_raw_messages SET processed_at = $1 WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	return err
}

func (s *PGListenRawMessageStore) ListPendingGroups(ctx context.Context) ([]store.ListenRawMessageGroup, error) {
	tClause, tArgs, _, err := scopeClause(ctx, 1)
	if err != nil {
		return nil, err
	}
	var result []store.ListenRawMessageGroup
	err = pkgSqlxDB.SelectContext(ctx, &result,
		`SELECT DISTINCT agent_id, graph_id FROM listen_raw_messages WHERE processed_at IS NULL`+tClause,
		tArgs...,
	)
	return result, err
}

func (s *PGListenRawMessageStore) ResetProcessed(ctx context.Context, agentID, graphID string) (int64, error) {
	var conditions []string
	var args []any
	idx := 1

	if agentID != "" {
		conditions = append(conditions, fmt.Sprintf("agent_id = $%d", idx))
		args = append(args, agentID)
		idx++
	}
	if graphID != "" {
		conditions = append(conditions, fmt.Sprintf("graph_id = $%d", idx))
		args = append(args, graphID)
		idx++
	}
	conditions = append(conditions, "processed_at IS NOT NULL")

	tClause, tArgs, _, err := scopeClause(ctx, idx)
	if err != nil {
		return 0, err
	}

	where := strings.Join(conditions, " AND ")
	q := `UPDATE listen_raw_messages SET processed_at = NULL WHERE ` + where + tClause
	args = append(args, tArgs...)

	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *PGListenRawMessageStore) ResetProcessedByIDs(ctx context.Context, ids []uuid.UUID) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+2)
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args = append(args, id)
	}
	idx := len(args) + 1
	tClause, tArgs, _, err := scopeClause(ctx, idx)
	if err != nil {
		return 0, err
	}
	args = append(args, tArgs...)
	q := `UPDATE listen_raw_messages SET processed_at = NULL WHERE id IN (` + strings.Join(placeholders, ",") + `) AND processed_at IS NOT NULL` + tClause
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *PGListenRawMessageStore) List(ctx context.Context, opts store.ListenRawMessageListOpts) ([]store.ListenRawMessage, int, error) {
	tClause, tArgs, _, err := scopeClause(ctx, 1)
	if err != nil {
		return nil, 0, err
	}

	// Build alias-qualified scope clause for the JOIN query.
	tmClause, _, _, err := scopeClauseAlias(ctx, 1, "m")
	if err != nil {
		return nil, 0, err
	}

	var where []string
	var whereM []string // same conditions qualified with m. for the JOIN query
	var args []any
	paramIdx := len(tArgs) + 1 // params start after tenant scope args

	if opts.ChannelName != "" {
		where = append(where, fmt.Sprintf("channel_name = $%d", paramIdx))
		whereM = append(whereM, fmt.Sprintf("m.channel_name = $%d", paramIdx))
		args = append(args, opts.ChannelName)
		paramIdx++
	}
	if opts.ChatID != "" {
		where = append(where, fmt.Sprintf("chat_id = $%d", paramIdx))
		whereM = append(whereM, fmt.Sprintf("m.chat_id = $%d", paramIdx))
		args = append(args, opts.ChatID)
		paramIdx++
	}
	if opts.AgentID != "" {
		where = append(where, fmt.Sprintf("agent_id = $%d", paramIdx))
		whereM = append(whereM, fmt.Sprintf("m.agent_id = $%d", paramIdx))
		args = append(args, opts.AgentID)
		paramIdx++
	}
	if opts.GraphID != "" {
		where = append(where, fmt.Sprintf("graph_id = $%d", paramIdx))
		whereM = append(whereM, fmt.Sprintf("m.graph_id = $%d", paramIdx))
		args = append(args, opts.GraphID)
		paramIdx++
	}
	if opts.Processed != nil {
		if *opts.Processed {
			where = append(where, "processed_at IS NOT NULL")
			whereM = append(whereM, "m.processed_at IS NOT NULL")
		} else {
			where = append(where, "processed_at IS NULL")
			whereM = append(whereM, "m.processed_at IS NULL")
		}
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = " AND " + strings.Join(where, " AND ")
	}
	whereMClause := ""
	if len(whereM) > 0 {
		whereMClause = " AND " + strings.Join(whereM, " AND ")
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
	offset := max(opts.Offset, 0)
	pageArgs := append(tArgs, args...)
	pageArgs = append(pageArgs, limit, offset)

	var rows []rawMsgRow
	err = pkgSqlxDB.SelectContext(ctx, &rows,
		`SELECT m.id, m.channel_name, m.chat_id, m.chat_name, m.graph_id, m.sender, m.sender_id, m.body, m.msg_timestamp, m.agent_id, m.created_at, m.processed_at, m.media_refs,
				        COALESCE(a.display_name, a.agent_key, '') AS agent_name
				 FROM listen_raw_messages m
				 LEFT JOIN agents a ON a.id = m.agent_id
				 WHERE 1=1`+tmClause+whereMClause+`
				 ORDER BY m.created_at DESC
				 LIMIT $`+fmt.Sprintf("%d", paramIdx)+` OFFSET $`+fmt.Sprintf("%d", paramIdx+1),
		pageArgs...,
	)
	if err != nil {
		return nil, total, err
	}
	result := make([]store.ListenRawMessage, len(rows))
	for i, r := range rows {
		result[i] = r.toMessage()
	}
	return result, total, nil
}

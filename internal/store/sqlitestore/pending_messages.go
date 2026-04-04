//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLitePendingMessageStore implements store.PendingMessageStore backed by SQLite.
type SQLitePendingMessageStore struct {
	db *sql.DB
}

func NewSQLitePendingMessageStore(db *sql.DB) *SQLitePendingMessageStore {
	return &SQLitePendingMessageStore{db: db}
}

func (s *SQLitePendingMessageStore) AppendBatch(ctx context.Context, msgs []store.PendingMessage) error {
	if len(msgs) == 0 {
		return nil
	}

	const cols = 11
	placeholders := make([]string, len(msgs))
	args := make([]any, 0, len(msgs)*cols)
	now := time.Now()
	tid := tenantIDForInsert(ctx)

	for i := range msgs {
		if msgs[i].ID == uuid.Nil {
			msgs[i].ID = uuid.Must(uuid.NewV7())
		}
		placeholders[i] = "(?,?,?,?,?,?,?,?,?,?,?)"
		args = append(args, msgs[i].ID, msgs[i].ChannelName, msgs[i].HistoryKey,
			msgs[i].Sender, msgs[i].SenderID, msgs[i].Body, msgs[i].PlatformMsgID, msgs[i].IsSummary, now, now, tid)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO channel_pending_messages (id, channel_name, history_key, sender, sender_id, body, platform_msg_id, is_summary, created_at, updated_at, tenant_id)
		 VALUES `+strings.Join(placeholders, ","),
		args...,
	)
	return err
}

func (s *SQLitePendingMessageStore) ListByKey(ctx context.Context, channelName, historyKey string) ([]store.PendingMessage, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	args := append([]any{channelName, historyKey}, tArgs...)
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, channel_name, history_key, sender, sender_id, body, platform_msg_id, is_summary, created_at, updated_at
		 FROM channel_pending_messages
		 WHERE channel_name = ? AND history_key = ?`+tClause+`
		 ORDER BY created_at ASC`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.PendingMessage
	for rows.Next() {
		var m store.PendingMessage
		createdAt, updatedAt := scanTimePair()
		if err := rows.Scan(&m.ID, &m.ChannelName, &m.HistoryKey, &m.Sender, &m.SenderID, &m.Body, &m.PlatformMsgID, &m.IsSummary, createdAt, updatedAt); err != nil {
			return nil, err
		}
		m.CreatedAt = createdAt.Time
		m.UpdatedAt = updatedAt.Time
		result = append(result, m)
	}
	return result, rows.Err()
}

func (s *SQLitePendingMessageStore) DeleteByKey(ctx context.Context, channelName, historyKey string) error {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return err
	}
	args := append([]any{channelName, historyKey}, tArgs...)
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM channel_pending_messages WHERE channel_name = ? AND history_key = ?`+tClause,
		args...,
	)
	return err
}

func (s *SQLitePendingMessageStore) Compact(ctx context.Context, deleteIDs []uuid.UUID, summary *store.PendingMessage) error {
	if len(deleteIDs) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin compact tx: %w", err)
	}
	defer tx.Rollback()

	placeholders := make([]string, len(deleteIDs))
	args := make([]any, len(deleteIDs))
	for i, id := range deleteIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	res, err := tx.ExecContext(ctx,
		fmt.Sprintf("DELETE FROM channel_pending_messages WHERE id IN (%s)", strings.Join(placeholders, ",")),
		args...,
	)
	if err != nil {
		return fmt.Errorf("compact delete: %w", err)
	}

	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil
	}

	if summary.ID == uuid.Nil {
		summary.ID = uuid.Must(uuid.NewV7())
	}
	now := time.Now()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO channel_pending_messages (id, channel_name, history_key, sender, sender_id, body, platform_msg_id, is_summary, created_at, updated_at, tenant_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		summary.ID, summary.ChannelName, summary.HistoryKey, summary.Sender, summary.SenderID, summary.Body, summary.PlatformMsgID, true, now, now, tenantIDForInsert(ctx),
	)
	if err != nil {
		return fmt.Errorf("compact insert summary: %w", err)
	}

	return tx.Commit()
}

func (s *SQLitePendingMessageStore) DeleteStale(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	tid := tenantIDForInsert(ctx)
	result, err := s.db.ExecContext(ctx,
		`DELETE FROM channel_pending_messages WHERE updated_at < ? AND tenant_id = ?`,
		cutoff, tid,
	)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *SQLitePendingMessageStore) ListGroups(ctx context.Context) ([]store.PendingMessageGroup, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return nil, err
	}
	// SQLite: BOOL_OR → MAX(is_summary), EXISTS subquery logic preserved
	q := `SELECT channel_name, history_key,
		        COUNT(*) AS message_count,
		        MAX(is_summary) AND NOT EXISTS (
		            SELECT 1 FROM channel_pending_messages n
		            WHERE n.channel_name = m.channel_name
		              AND n.history_key  = m.history_key
		              AND NOT n.is_summary
		              AND n.created_at > (
		                SELECT MAX(s.created_at)
		                FROM channel_pending_messages s
		                WHERE s.channel_name = m.channel_name
		                  AND s.history_key  = m.history_key
		                  AND s.is_summary
		              )
		          ) AS has_summary,
		        MAX(created_at) AS last_activity
		 FROM channel_pending_messages m
		 WHERE 1=1` + tClause + `
		 GROUP BY channel_name, history_key
		 ORDER BY last_activity DESC`

	rows, err := s.db.QueryContext(ctx, q, tArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.PendingMessageGroup
	for rows.Next() {
		var g store.PendingMessageGroup
		if err := rows.Scan(&g.ChannelName, &g.HistoryKey, &g.MessageCount, &g.HasSummary, &g.LastActivity); err != nil {
			return nil, err
		}
		result = append(result, g)
	}
	return result, rows.Err()
}

func (s *SQLitePendingMessageStore) CountAll(ctx context.Context) (int64, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return 0, err
	}
	var count int64
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM channel_pending_messages WHERE 1=1`+tClause,
		tArgs...,
	).Scan(&count)
	return count, err
}

func (s *SQLitePendingMessageStore) CountByKey(ctx context.Context, channelName, historyKey string) (int, error) {
	tClause, tArgs, err := scopeClause(ctx)
	if err != nil {
		return 0, err
	}
	args := append([]any{channelName, historyKey}, tArgs...)
	var count int
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM channel_pending_messages WHERE channel_name = ? AND history_key = ?`+tClause,
		args...,
	).Scan(&count)
	return count, err
}

func (s *SQLitePendingMessageStore) ResolveGroupTitles(ctx context.Context, groups []store.PendingMessageGroup) (map[string]string, error) {
	if len(groups) == 0 {
		return nil, nil
	}

	// Build OR conditions using LIKE with ? placeholders
	conditions := make([]string, 0, len(groups))
	args := make([]any, 0, len(groups)*2)
	for _, g := range groups {
		conditions = append(conditions, "(session_key LIKE '%:' || ? || ':group:' || ? || '%')")
		args = append(args, g.ChannelName, g.HistoryKey)
	}

	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			tid = store.MasterTenantID
		}
		args = append(args, tid)
	}

	tenantFilter := ""
	if !store.IsCrossTenant(ctx) {
		tenantFilter = " AND tenant_id = ?"
	}

	rows, err := s.db.QueryContext(ctx,
		"SELECT session_key, json_extract(metadata, '$.chat_title')"+
			" FROM sessions"+
			" WHERE json_extract(metadata, '$.chat_title') != ''"+
			" AND ("+strings.Join(conditions, " OR ")+")"+tenantFilter,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var sessionKey, title string
		if err := rows.Scan(&sessionKey, &title); err != nil {
			return nil, err
		}
		for _, g := range groups {
			pattern := ":" + g.ChannelName + ":group:" + g.HistoryKey
			if strings.Contains(sessionKey, pattern) {
				mapKey := g.ChannelName + ":" + g.HistoryKey
				if _, exists := result[mapKey]; !exists {
					result[mapKey] = title
				}
				break
			}
		}
	}
	return result, rows.Err()
}

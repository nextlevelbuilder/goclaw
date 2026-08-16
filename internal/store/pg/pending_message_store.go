package pg

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGPendingMessageStore implements store.PendingMessageStore backed by Postgres.
type PGPendingMessageStore struct {
	db *sql.DB
}

// NewPGPendingMessageStore creates a new PGPendingMessageStore.
func NewPGPendingMessageStore(db *sql.DB) *PGPendingMessageStore {
	return &PGPendingMessageStore{db: db}
}

func (s *PGPendingMessageStore) AppendBatch(ctx context.Context, msgs []store.PendingMessage) error {
	if len(msgs) == 0 {
		return nil
	}

	// Build multi-row INSERT: VALUES ($1,$2,...,$12), ($13,$14,...), ...
	const cols = 12
	placeholders := make([]string, len(msgs))
	args := make([]any, 0, len(msgs)*cols)
	now := time.Now()
	tid := tenantIDForInsert(ctx)

	for i := range msgs {
		if msgs[i].ID == uuid.Nil {
			msgs[i].ID = uuid.Must(uuid.NewV7())
		}
		base := i * cols
		placeholders[i] = fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8, base+9, base+10, base+11, base+12)
		args = append(args, msgs[i].ID, msgs[i].ChannelName, msgs[i].HistoryKey, msgs[i].ParentHistoryKey,
			msgs[i].Sender, msgs[i].SenderID, msgs[i].Body, msgs[i].PlatformMsgID, msgs[i].IsSummary, now, now, tid)
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO channel_pending_messages (id, channel_name, history_key, parent_history_key, sender, sender_id, body, platform_msg_id, is_summary, created_at, updated_at, tenant_id)
		 VALUES `+strings.Join(placeholders, ","),
		args...,
	)
	return err
}

func (s *PGPendingMessageStore) ListByKey(ctx context.Context, channelName, historyKey string) ([]store.PendingMessage, error) {
	tClause, tArgs, _, err := scopeClause(ctx, 3)
	if err != nil {
		return nil, err
	}
	var result []store.PendingMessage
	err = pkgSqlxDB.SelectContext(ctx, &result,
		`SELECT id, channel_name, history_key, parent_history_key, sender, sender_id, body, platform_msg_id, is_summary, created_at, updated_at
		 FROM channel_pending_messages
		 WHERE channel_name = $1 AND history_key = $2`+tClause+`
		 ORDER BY created_at ASC, id ASC`,
		append([]any{channelName, historyKey}, tArgs...)...,
	)
	return result, err
}

// archivedColumns are copied verbatim from the buffer into channel_message_archive.
const archivedColumns = `id, channel_name, history_key, parent_history_key, sender, sender_id,
	body, platform_msg_id, is_summary, created_at, updated_at, tenant_id`

// archivedReadColumns omits tenant_id: reads are already tenant-scoped by the
// WHERE clause and ArchivedMessage carries no tenant field.
const archivedReadColumns = `id, channel_name, history_key, parent_history_key, sender, sender_id,
	body, platform_msg_id, is_summary, created_at, updated_at, archived_at, archive_reason`

// archivePending copies every buffer row matching where into the archive. Callers
// pass the same where/args they are about to DELETE with, so the archive cannot
// miss a row the delete removes. Replaying is a no-op: archived rows keep their
// original id.
func archivePending(ctx context.Context, tx *sql.Tx, where string, args []any, reasonParam int, reason string) error {
	insertArgs := make([]any, 0, len(args)+1)
	insertArgs = append(insertArgs, args...)
	insertArgs = append(insertArgs, reason)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO channel_message_archive (`+archivedColumns+`, archived_at, archive_reason)
		 SELECT `+archivedColumns+fmt.Sprintf(", NOW(), $%d", reasonParam)+`
		 FROM channel_pending_messages `+where+`
		 ON CONFLICT (id) DO NOTHING`,
		insertArgs...,
	); err != nil {
		return fmt.Errorf("archive pending: %w", err)
	}
	return nil
}

func (s *PGPendingMessageStore) DeleteByKey(ctx context.Context, channelName, historyKey string) error {
	tClause, tArgs, nextParam, err := scopeClause(ctx, 3)
	if err != nil {
		return err
	}
	args := append([]any{channelName, historyKey}, tArgs...)
	where := `WHERE channel_name = $1 AND history_key = $2` + tClause

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete tx: %w", err)
	}
	defer tx.Rollback()

	if err := archivePending(ctx, tx, where, args, nextParam, store.ArchiveReasonConsumed); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM channel_pending_messages `+where, args...); err != nil {
		return fmt.Errorf("delete pending: %w", err)
	}
	return tx.Commit()
}

func (s *PGPendingMessageStore) Compact(ctx context.Context, deleteIDs []uuid.UUID, summary *store.PendingMessage) error {
	if len(deleteIDs) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin compact tx: %w", err)
	}
	defer tx.Rollback()

	// Build placeholder list for DELETE IN clause
	placeholders := make([]string, len(deleteIDs))
	args := make([]any, len(deleteIDs))
	for i, id := range deleteIDs {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	where := fmt.Sprintf("WHERE id IN (%s)", strings.Join(placeholders, ","))
	if err := archivePending(ctx, tx, where, args, len(deleteIDs)+1, store.ArchiveReasonCompacted); err != nil {
		return err
	}

	res, err := tx.ExecContext(ctx, "DELETE FROM channel_pending_messages "+where, args...)
	if err != nil {
		return fmt.Errorf("compact delete: %w", err)
	}

	// Guard: if another compaction already deleted these rows, skip summary insertion
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return nil // already compacted by concurrent caller
	}

	// Insert summary row
	if summary.ID == uuid.Nil {
		summary.ID = uuid.Must(uuid.NewV7())
	}
	now := time.Now()
	_, err = tx.ExecContext(ctx,
		`INSERT INTO channel_pending_messages (id, channel_name, history_key, parent_history_key, sender, sender_id, body, platform_msg_id, is_summary, created_at, updated_at, tenant_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		summary.ID, summary.ChannelName, summary.HistoryKey, summary.ParentHistoryKey, summary.Sender, summary.SenderID, summary.Body, summary.PlatformMsgID, true, now, now, tenantIDForInsert(ctx),
	)
	if err != nil {
		return fmt.Errorf("compact insert summary: %w", err)
	}

	return tx.Commit()
}

func (s *PGPendingMessageStore) DeleteStale(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	args := []any{cutoff, tenantIDForInsert(ctx)}
	where := `WHERE updated_at < $1 AND tenant_id = $2`

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin delete stale tx: %w", err)
	}
	defer tx.Rollback()

	if err := archivePending(ctx, tx, where, args, len(args)+1, store.ArchiveReasonStale); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM channel_pending_messages `+where, args...)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return affected, nil
}

func (s *PGPendingMessageStore) ListArchivedByKey(ctx context.Context, channelName, historyKey string, since time.Time, limit int) ([]store.ArchivedMessage, error) {
	tClause, tArgs, nextParam, err := scopeClause(ctx, 3)
	if err != nil {
		return nil, err
	}
	args := append([]any{channelName, historyKey}, tArgs...)
	query := `SELECT ` + archivedReadColumns + `
		 FROM channel_message_archive
		 WHERE channel_name = $1 AND history_key = $2` + tClause
	if !since.IsZero() {
		query += fmt.Sprintf(" AND created_at >= $%d", nextParam)
		args = append(args, since)
		nextParam++
	}
	query += ` ORDER BY created_at ASC, id ASC`
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT $%d", nextParam)
		args = append(args, limit)
	}

	var result []store.ArchivedMessage
	err = pkgSqlxDB.SelectContext(ctx, &result, query, args...)
	return result, err
}

func (s *PGPendingMessageStore) ListGroups(ctx context.Context) ([]store.PendingMessageGroup, error) {
	tClause, tArgs, _, err := scopeClause(ctx, 1)
	if err != nil {
		return nil, err
	}
	var where string
	if tClause != "" {
		where = ` WHERE m.tenant_id = $1`
	}
	var result []store.PendingMessageGroup
	err = pkgSqlxDB.SelectContext(ctx, &result,
		`SELECT channel_name, history_key, MAX(parent_history_key) AS parent_history_key,
		        COUNT(*) AS message_count,
		        BOOL_OR(is_summary)
		          AND NOT EXISTS (
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
		 FROM channel_pending_messages m`+where+`
		 GROUP BY channel_name, history_key
		 ORDER BY last_activity DESC`,
		tArgs...,
	)
	return result, err
}

func (s *PGPendingMessageStore) CountAll(ctx context.Context) (int64, error) {
	tClause, tArgs, _, err := scopeClause(ctx, 1)
	if err != nil {
		return 0, err
	}
	var count int64
	var query string
	if tClause != "" {
		query = `SELECT COUNT(*) FROM channel_pending_messages WHERE tenant_id = $1`
	} else {
		query = `SELECT COUNT(*) FROM channel_pending_messages`
	}
	err = s.db.QueryRowContext(ctx, query, tArgs...).Scan(&count)
	return count, err
}

func (s *PGPendingMessageStore) CountByKey(ctx context.Context, channelName, historyKey string) (int, error) {
	tClause, tArgs, _, err := scopeClause(ctx, 3)
	if err != nil {
		return 0, err
	}
	var count int
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM channel_pending_messages WHERE channel_name = $1 AND history_key = $2`+tClause,
		append([]any{channelName, historyKey}, tArgs...)...,
	).Scan(&count)
	return count, err
}

func (s *PGPendingMessageStore) ResolveGroupTitles(ctx context.Context, groups []store.PendingMessageGroup) (map[string]string, error) {
	if len(groups) == 0 {
		return nil, nil
	}

	result, missing := s.resolveGroupTitlesFromContacts(ctx, groups)
	if len(missing) == 0 {
		return result, nil
	}

	// Build OR conditions: session_key LIKE '%:{channel}:group:{key}%'
	conditions := make([]string, 0, len(missing))
	args := make([]any, 0, len(missing)*2)
	for i, g := range missing {
		conditions = append(conditions, fmt.Sprintf(
			"(session_key LIKE '%%:' || $%d || ':group:' || $%d || '%%')",
			i*2+1, i*2+2,
		))
		args = append(args, g.ChannelName, g.HistoryKey)
	}

	tenantFilter := ""
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			tid = store.MasterTenantID
		}
		argIdx := len(args) + 1
		tenantFilter = fmt.Sprintf(" AND tenant_id = $%d", argIdx)
		args = append(args, tid)
	}

	rows, err := s.db.QueryContext(ctx,
		"SELECT session_key, metadata->>'chat_title'"+
			" FROM sessions"+
			" WHERE metadata->>'chat_title' != ''"+
			" AND ("+strings.Join(conditions, " OR ")+")"+tenantFilter,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var sessionKey, title string
		if err := rows.Scan(&sessionKey, &title); err != nil {
			return nil, err
		}
		// Match session_key back to channel:key pair
		for _, g := range missing {
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

func (s *PGPendingMessageStore) resolveGroupTitlesFromContacts(ctx context.Context, groups []store.PendingMessageGroup) (map[string]string, []store.PendingMessageGroup) {
	result := make(map[string]string, len(groups))
	unique := uniquePendingTitleGroups(groups)
	if len(unique) == 0 {
		return result, nil
	}

	conditions := make([]string, 0, len(unique))
	args := make([]any, 0, len(unique)*2+1)
	for i, g := range unique {
		conditions = append(conditions, fmt.Sprintf("(channel_instance = $%d AND sender_id = $%d)", i*2+1, i*2+2))
		args = append(args, g.ChannelName, g.HistoryKey)
	}

	tenantFilter := ""
	if !store.IsCrossTenant(ctx) {
		tid := store.TenantIDFromContext(ctx)
		if tid == uuid.Nil {
			tid = store.MasterTenantID
		}
		tenantFilter = fmt.Sprintf(" AND tenant_id = $%d", len(args)+1)
		args = append(args, tid)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT channel_instance, sender_id, COALESCE(NULLIF(metadata->>'display_title', ''), display_name)
		 FROM channel_contacts
		 WHERE display_name IS NOT NULL
		   AND display_name <> ''
		   AND contact_type IN ('group', 'topic')
		   AND (`+strings.Join(conditions, " OR ")+`)`+tenantFilter,
		args...,
	)
	if err != nil {
		return result, unique
	}
	defer rows.Close()

	for rows.Next() {
		var channelName, senderID, title string
		if err := rows.Scan(&channelName, &senderID, &title); err != nil {
			return result, unique
		}
		result[channelName+":"+senderID] = title
	}
	if err := rows.Err(); err != nil {
		return result, unique
	}

	missing := make([]store.PendingMessageGroup, 0, len(unique)-len(result))
	for _, g := range unique {
		if result[g.ChannelName+":"+g.HistoryKey] == "" {
			missing = append(missing, g)
		}
	}
	return result, missing
}

func uniquePendingTitleGroups(groups []store.PendingMessageGroup) []store.PendingMessageGroup {
	out := make([]store.PendingMessageGroup, 0, len(groups))
	seen := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		if g.ChannelName == "" || g.HistoryKey == "" {
			continue
		}
		key := g.ChannelName + ":" + g.HistoryKey
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, g)
	}
	return out
}

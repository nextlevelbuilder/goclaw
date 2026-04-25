//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteDailyDigestStore implements store.DailyDigestStore backed by SQLite.
type SQLiteDailyDigestStore struct {
	db *sql.DB
}

// NewSQLiteDailyDigestStore creates a new SQLite-backed daily digest store.
func NewSQLiteDailyDigestStore(db *sql.DB) *SQLiteDailyDigestStore {
	return &SQLiteDailyDigestStore{db: db}
}

// Upsert creates or updates a daily digest for the given date and scope.
func (s *SQLiteDailyDigestStore) Upsert(ctx context.Context, digest *store.DailyDigest) error {
	if digest.ID == uuid.Nil {
		digest.ID = uuid.Must(uuid.NewV7())
	}
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)

	decisionsJSON, _ := json.Marshal(digest.Decisions)
	actionsJSON, _ := json.Marshal(digest.ActionItems)
	topicsJSON := jsonStringArray(digest.KeyTopics)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO daily_digests
			(id, tenant_id, agent_id, user_id, channel_scope, session_key_prefix,
			 digest_date, decisions, action_items, key_topics, summary,
			 session_count, message_count, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (tenant_id, agent_id, user_id, digest_date, channel_scope)
		DO UPDATE SET
			decisions = excluded.decisions,
			action_items = excluded.action_items,
			key_topics = excluded.key_topics,
			summary = excluded.summary,
			session_count = excluded.session_count,
			message_count = excluded.message_count,
			session_key_prefix = excluded.session_key_prefix,
			updated_at = excluded.updated_at`,
		digest.ID.String(), digest.TenantID.String(), digest.AgentID.String(),
		digest.UserID, digest.ChannelScope, digest.SessionKeyPrefix,
		digest.DigestDate.Format("2006-01-02"),
		string(decisionsJSON), string(actionsJSON), topicsJSON,
		digest.Summary, digest.SessionCount, digest.MessageCount, nowStr, nowStr)
	if err != nil {
		return fmt.Errorf("daily_digest upsert: %w", err)
	}
	digest.CreatedAt = now
	digest.UpdatedAt = now
	return nil
}

// Get retrieves a daily digest by agent, user, date, and optional scope.
func (s *SQLiteDailyDigestStore) Get(ctx context.Context, agentID, userID string, date time.Time, channelScope string) (*store.DailyDigest, error) {
	tenantID := tenantIDForInsert(ctx)
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, agent_id, user_id, channel_scope, session_key_prefix,
		       digest_date, decisions, action_items, key_topics, summary,
		       session_count, message_count, created_at, updated_at
		FROM daily_digests
		WHERE agent_id = ? AND user_id = ? AND digest_date = ?
		  AND channel_scope = ? AND tenant_id = ?`,
		agentID, userID, date.Format("2006-01-02"), channelScope, tenantID.String())
	return scanSQLiteDailyDigest(row)
}

// List returns recent daily digests for an agent/user.
func (s *SQLiteDailyDigestStore) List(ctx context.Context, agentID, userID string, limit int) ([]store.DailyDigest, error) {
	if limit <= 0 {
		limit = 30
	}
	tenantID := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, agent_id, user_id, channel_scope, session_key_prefix,
		       digest_date, decisions, action_items, key_topics, summary,
		       session_count, message_count, created_at, updated_at
		FROM daily_digests
		WHERE agent_id = ? AND user_id = ? AND tenant_id = ?
		ORDER BY digest_date DESC LIMIT ?`,
		agentID, userID, tenantID.String(), limit)
	if err != nil {
		return nil, fmt.Errorf("daily_digest list: %w", err)
	}
	defer rows.Close()
	return scanSQLiteDailyDigestRows(rows)
}

// ListByDateRange returns digests within a date range.
func (s *SQLiteDailyDigestStore) ListByDateRange(ctx context.Context, agentID, userID string, from, to time.Time) ([]store.DailyDigest, error) {
	tenantID := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, agent_id, user_id, channel_scope, session_key_prefix,
		       digest_date, decisions, action_items, key_topics, summary,
		       session_count, message_count, created_at, updated_at
		FROM daily_digests
		WHERE agent_id = ? AND user_id = ? AND tenant_id = ?
		  AND digest_date >= ? AND digest_date <= ?
		ORDER BY digest_date DESC`,
		agentID, userID, tenantID.String(), from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("daily_digest list_by_range: %w", err)
	}
	defer rows.Close()
	return scanSQLiteDailyDigestRows(rows)
}

func scanSQLiteDailyDigest(row *sql.Row) (*store.DailyDigest, error) {
	var d store.DailyDigest
	var idStr, tenantStr, agentStr string
	var decisionsBytes, actionsBytes, topicsBytes []byte
	var digestDate, createdAt, updatedAt sqliteTime
	err := row.Scan(
		&idStr, &tenantStr, &agentStr, &d.UserID, &d.ChannelScope, &d.SessionKeyPrefix,
		&digestDate, &decisionsBytes, &actionsBytes, &topicsBytes, &d.Summary,
		&d.SessionCount, &d.MessageCount, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.ID, _ = uuid.Parse(idStr)
	d.TenantID, _ = uuid.Parse(tenantStr)
	d.AgentID, _ = uuid.Parse(agentStr)
	d.DigestDate = digestDate.Time
	d.CreatedAt = createdAt.Time
	d.UpdatedAt = updatedAt.Time
	_ = json.Unmarshal(decisionsBytes, &d.Decisions)
	_ = json.Unmarshal(actionsBytes, &d.ActionItems)
	scanJSONStringArray(topicsBytes, &d.KeyTopics)
	return &d, nil
}

func scanSQLiteDailyDigestRows(rows *sql.Rows) ([]store.DailyDigest, error) {
	var results []store.DailyDigest
	for rows.Next() {
		var d store.DailyDigest
		var idStr, tenantStr, agentStr string
		var decisionsBytes, actionsBytes, topicsBytes []byte
		var digestDate, createdAt, updatedAt sqliteTime
		if err := rows.Scan(
			&idStr, &tenantStr, &agentStr, &d.UserID, &d.ChannelScope, &d.SessionKeyPrefix,
			&digestDate, &decisionsBytes, &actionsBytes, &topicsBytes, &d.Summary,
			&d.SessionCount, &d.MessageCount, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		d.ID, _ = uuid.Parse(idStr)
		d.TenantID, _ = uuid.Parse(tenantStr)
		d.AgentID, _ = uuid.Parse(agentStr)
		d.DigestDate = digestDate.Time
		d.CreatedAt = createdAt.Time
		d.UpdatedAt = updatedAt.Time
		_ = json.Unmarshal(decisionsBytes, &d.Decisions)
		_ = json.Unmarshal(actionsBytes, &d.ActionItems)
		scanJSONStringArray(topicsBytes, &d.KeyTopics)
		results = append(results, d)
	}
	return results, rows.Err()
}

// Ensure SQLiteDailyDigestStore implements store.DailyDigestStore.
var _ store.DailyDigestStore = (*SQLiteDailyDigestStore)(nil)

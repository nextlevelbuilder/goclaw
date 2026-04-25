package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGDailyDigestStore implements store.DailyDigestStore backed by PostgreSQL.
type PGDailyDigestStore struct {
	db *sql.DB
}

// NewPGDailyDigestStore creates a new PG-backed daily digest store.
func NewPGDailyDigestStore(db *sql.DB) *PGDailyDigestStore {
	return &PGDailyDigestStore{db: db}
}

// Upsert creates or updates a daily digest for the given date and scope.
func (s *PGDailyDigestStore) Upsert(ctx context.Context, digest *store.DailyDigest) error {
	if digest.ID == uuid.Nil {
		digest.ID = uuid.Must(uuid.NewV7())
	}
	now := time.Now().UTC()

	decisionsJSON, _ := json.Marshal(digest.Decisions)
	actionsJSON, _ := json.Marshal(digest.ActionItems)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO daily_digests
			(id, tenant_id, agent_id, user_id, channel_scope, session_key_prefix,
			 digest_date, decisions, action_items, key_topics, summary,
			 session_count, message_count, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $14)
		ON CONFLICT (tenant_id, agent_id, user_id, digest_date, channel_scope)
		DO UPDATE SET
			decisions = $8,
			action_items = $9,
			key_topics = $10,
			summary = $11,
			session_count = $12,
			message_count = $13,
			session_key_prefix = $6,
			updated_at = $14`,
		digest.ID, digest.TenantID, digest.AgentID, digest.UserID,
		digest.ChannelScope, digest.SessionKeyPrefix, digest.DigestDate,
		decisionsJSON, actionsJSON, pq.Array(digest.KeyTopics),
		digest.Summary, digest.SessionCount, digest.MessageCount, now)
	if err != nil {
		return fmt.Errorf("daily_digest upsert: %w", err)
	}
	digest.CreatedAt = now
	digest.UpdatedAt = now
	return nil
}

// Get retrieves a daily digest by agent, user, date, and optional scope.
func (s *PGDailyDigestStore) Get(ctx context.Context, agentID, userID string, date time.Time, channelScope string) (*store.DailyDigest, error) {
	tenantID := store.TenantIDFromContext(ctx)
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, agent_id, user_id, channel_scope, session_key_prefix,
		       digest_date, decisions, action_items, key_topics, summary,
		       session_count, message_count, created_at, updated_at
		FROM daily_digests
		WHERE agent_id = $1 AND user_id = $2 AND digest_date = $3
		  AND channel_scope = $4 AND tenant_id = $5`,
		agentID, userID, date.Format("2006-01-02"), channelScope, tenantID)
	return scanDailyDigest(row)
}

// List returns recent daily digests for an agent/user.
func (s *PGDailyDigestStore) List(ctx context.Context, agentID, userID string, limit int) ([]store.DailyDigest, error) {
	if limit <= 0 {
		limit = 30
	}
	tenantID := store.TenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, agent_id, user_id, channel_scope, session_key_prefix,
		       digest_date, decisions, action_items, key_topics, summary,
		       session_count, message_count, created_at, updated_at
		FROM daily_digests
		WHERE agent_id = $1 AND user_id = $2 AND tenant_id = $3
		ORDER BY digest_date DESC LIMIT $4`,
		agentID, userID, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("daily_digest list: %w", err)
	}
	defer rows.Close()
	return scanDailyDigestRows(rows)
}

// ListByDateRange returns digests within a date range.
func (s *PGDailyDigestStore) ListByDateRange(ctx context.Context, agentID, userID string, from, to time.Time) ([]store.DailyDigest, error) {
	tenantID := store.TenantIDFromContext(ctx)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, agent_id, user_id, channel_scope, session_key_prefix,
		       digest_date, decisions, action_items, key_topics, summary,
		       session_count, message_count, created_at, updated_at
		FROM daily_digests
		WHERE agent_id = $1 AND user_id = $2 AND tenant_id = $3
		  AND digest_date >= $4 AND digest_date <= $5
		ORDER BY digest_date DESC`,
		agentID, userID, tenantID, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, fmt.Errorf("daily_digest list_by_range: %w", err)
	}
	defer rows.Close()
	return scanDailyDigestRows(rows)
}

func scanDailyDigest(row *sql.Row) (*store.DailyDigest, error) {
	var d store.DailyDigest
	var decisionsBytes, actionsBytes []byte
	var topics pq.StringArray
	err := row.Scan(
		&d.ID, &d.TenantID, &d.AgentID, &d.UserID, &d.ChannelScope, &d.SessionKeyPrefix,
		&d.DigestDate, &decisionsBytes, &actionsBytes, &topics, &d.Summary,
		&d.SessionCount, &d.MessageCount, &d.CreatedAt, &d.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(decisionsBytes, &d.Decisions)
	_ = json.Unmarshal(actionsBytes, &d.ActionItems)
	d.KeyTopics = []string(topics)
	return &d, nil
}

func scanDailyDigestRows(rows *sql.Rows) ([]store.DailyDigest, error) {
	var results []store.DailyDigest
	for rows.Next() {
		var d store.DailyDigest
		var decisionsBytes, actionsBytes []byte
		var topics pq.StringArray
		if err := rows.Scan(
			&d.ID, &d.TenantID, &d.AgentID, &d.UserID, &d.ChannelScope, &d.SessionKeyPrefix,
			&d.DigestDate, &decisionsBytes, &actionsBytes, &topics, &d.Summary,
			&d.SessionCount, &d.MessageCount, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(decisionsBytes, &d.Decisions)
		_ = json.Unmarshal(actionsBytes, &d.ActionItems)
		d.KeyTopics = []string(topics)
		results = append(results, d)
	}
	return results, rows.Err()
}

// Ensure PGDailyDigestStore implements store.DailyDigestStore.
var _ store.DailyDigestStore = (*PGDailyDigestStore)(nil)

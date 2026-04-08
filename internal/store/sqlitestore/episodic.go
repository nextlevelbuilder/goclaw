//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SQLiteEpisodicStore implements store.EpisodicStore backed by SQLite.
type SQLiteEpisodicStore struct {
	db *sql.DB
}

// NewSQLiteEpisodicStore creates a new SQLite-backed episodic store.
func NewSQLiteEpisodicStore(db *sql.DB) *SQLiteEpisodicStore {
	return &SQLiteEpisodicStore{db: db}
}

// SetEmbeddingProvider is a no-op for SQLite (no vector search).
func (s *SQLiteEpisodicStore) SetEmbeddingProvider(_ store.EmbeddingProvider) {}

// Close is a no-op for SQLite.
func (s *SQLiteEpisodicStore) Close() error { return nil }

// Create inserts a new episodic summary.
func (s *SQLiteEpisodicStore) Create(ctx context.Context, ep *store.EpisodicSummary) error {
	id := uuid.Must(uuid.NewV7())
	ep.ID = id
	now := time.Now().UTC()

	topics := jsonStringArray(ep.KeyTopics)

	var expiresAt *string
	if ep.ExpiresAt != nil {
		v := ep.ExpiresAt.UTC().Format(time.RFC3339Nano)
		expiresAt = &v
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO episodic_summaries
			(id, tenant_id, agent_id, user_id, session_key, summary, key_topics,
			 turn_count, token_count, l0_abstract, source_id, source_type,
			 created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (agent_id, user_id, source_id) WHERE source_id IS NOT NULL DO NOTHING`,
		id.String(), ep.TenantID.String(), ep.AgentID.String(),
		ep.UserID, ep.SessionKey, ep.Summary, topics,
		ep.TurnCount, ep.TokenCount, ep.L0Abstract,
		ep.SourceID, ep.SourceType,
		now.Format(time.RFC3339Nano), expiresAt)
	if err != nil {
		return fmt.Errorf("episodic create: %w", err)
	}
	ep.CreatedAt = now
	return nil
}

// Get retrieves an episodic summary by ID.
func (s *SQLiteEpisodicStore) Get(ctx context.Context, id string) (*store.EpisodicSummary, error) {
	tenantID := tenantIDForInsert(ctx)
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, agent_id, user_id, session_key, summary, key_topics,
		       turn_count, token_count, l0_abstract, source_id, source_type,
		       created_at, expires_at
		FROM episodic_summaries WHERE id = ? AND tenant_id = ?`,
		id, tenantID.String())
	return scanSQLiteEpisodic(row)
}

// Delete removes an episodic summary.
func (s *SQLiteEpisodicStore) Delete(ctx context.Context, id string) error {
	tenantID := tenantIDForInsert(ctx)
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM episodic_summaries WHERE id = ? AND tenant_id = ?`,
		id, tenantID.String())
	return err
}

// List returns episodic summaries ordered by created_at DESC.
func (s *SQLiteEpisodicStore) List(ctx context.Context, agentID, userID string, limit, offset int) ([]store.EpisodicSummary, error) {
	if limit <= 0 {
		limit = 20
	}
	tenantID := tenantIDForInsert(ctx)

	var q string
	var args []any
	if userID != "" {
		q = `SELECT id, tenant_id, agent_id, user_id, session_key, summary, key_topics,
			       turn_count, token_count, l0_abstract, source_id, source_type,
			       created_at, expires_at
			FROM episodic_summaries
			WHERE agent_id = ? AND user_id = ? AND tenant_id = ?
			ORDER BY created_at DESC LIMIT ? OFFSET ?`
		args = []any{agentID, userID, tenantID.String(), limit, offset}
	} else {
		q = `SELECT id, tenant_id, agent_id, user_id, session_key, summary, key_topics,
			       turn_count, token_count, l0_abstract, source_id, source_type,
			       created_at, expires_at
			FROM episodic_summaries
			WHERE agent_id = ? AND tenant_id = ?
			ORDER BY created_at DESC LIMIT ? OFFSET ?`
		args = []any{agentID, tenantID.String(), limit, offset}
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanSQLiteEpisodicRows(rows)
}

// ExistsBySourceID checks if an episodic summary with the given source_id exists.
func (s *SQLiteEpisodicStore) ExistsBySourceID(ctx context.Context, agentID, userID, sourceID string) (bool, error) {
	tenantID := tenantIDForInsert(ctx)
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM episodic_summaries
		WHERE agent_id = ? AND user_id = ? AND source_id = ? AND tenant_id = ?)`,
		agentID, userID, sourceID, tenantID.String()).Scan(&exists)
	return exists, err
}

// PruneExpired deletes episodic summaries past their expiry.
func (s *SQLiteEpisodicStore) PruneExpired(ctx context.Context) (int, error) {
	tenantID := tenantIDForInsert(ctx)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM episodic_summaries
		WHERE expires_at IS NOT NULL AND expires_at < ? AND tenant_id = ?`,
		now, tenantID.String())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// ListUnpromoted returns summaries not yet promoted, oldest first.
func (s *SQLiteEpisodicStore) ListUnpromoted(ctx context.Context, agentID, userID string, limit int) ([]store.EpisodicSummary, error) {
	if limit <= 0 {
		limit = 20
	}
	tenantID := tenantIDForInsert(ctx)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, tenant_id, agent_id, user_id, session_key, summary, key_topics,
		       turn_count, token_count, l0_abstract, source_id, source_type,
		       created_at, expires_at
		FROM episodic_summaries
		WHERE agent_id = ? AND user_id = ? AND tenant_id = ? AND promoted_at IS NULL
		ORDER BY created_at ASC LIMIT ?`,
		agentID, userID, tenantID.String(), limit)
	if err != nil {
		return nil, fmt.Errorf("episodic list_unpromoted: %w", err)
	}
	defer rows.Close()
	return scanSQLiteEpisodicRows(rows)
}

// MarkPromoted sets promoted_at=now() for the given IDs.
// IDs are processed in chunks of 200 to avoid SQLite variable limit.
func (s *SQLiteEpisodicStore) MarkPromoted(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	const chunkSize = 200
	for i := 0; i < len(ids); i += chunkSize {
		end := i + chunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunk := ids[i:end]
		placeholders := strings.Repeat("?,", len(chunk)-1) + "?"
		args := make([]any, 0, len(chunk)+1)
		args = append(args, now)
		for _, id := range chunk {
			args = append(args, id)
		}
		_, err := s.db.ExecContext(ctx,
			`UPDATE episodic_summaries SET promoted_at = ? WHERE id IN (`+placeholders+`)`,
			args...)
		if err != nil {
			return fmt.Errorf("episodic mark_promoted: %w", err)
		}
	}
	return nil
}

// CountUnpromoted returns the count of unpromoted summaries for an agent/user.
func (s *SQLiteEpisodicStore) CountUnpromoted(ctx context.Context, agentID, userID string) (int, error) {
	tenantID := tenantIDForInsert(ctx)
	var count int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM episodic_summaries
		WHERE agent_id = ? AND user_id = ? AND tenant_id = ? AND promoted_at IS NULL`,
		agentID, userID, tenantID.String()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("episodic count_unpromoted: %w", err)
	}
	return count, nil
}

// scanSQLiteEpisodic scans a single row into EpisodicSummary.
func scanSQLiteEpisodic(row *sql.Row) (*store.EpisodicSummary, error) {
	var ep store.EpisodicSummary
	var idStr, tenantStr, agentStr string
	var topicsBytes []byte
	var createdAt sqliteTime
	var expiresAt nullSqliteTime
	err := row.Scan(
		&idStr, &tenantStr, &agentStr, &ep.UserID, &ep.SessionKey,
		&ep.Summary, &topicsBytes, &ep.TurnCount, &ep.TokenCount,
		&ep.L0Abstract, &ep.SourceID, &ep.SourceType,
		&createdAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ep.ID, _ = uuid.Parse(idStr)
	ep.TenantID, _ = uuid.Parse(tenantStr)
	ep.AgentID, _ = uuid.Parse(agentStr)
	scanJSONStringArray(topicsBytes, &ep.KeyTopics)
	ep.CreatedAt = createdAt.Time
	if expiresAt.Valid {
		t := expiresAt.Time
		ep.ExpiresAt = &t
	}
	return &ep, nil
}

// scanSQLiteEpisodicRows scans multiple rows into a slice of EpisodicSummary.
func scanSQLiteEpisodicRows(rows *sql.Rows) ([]store.EpisodicSummary, error) {
	var results []store.EpisodicSummary
	for rows.Next() {
		var ep store.EpisodicSummary
		var idStr, tenantStr, agentStr string
		var topicsBytes []byte
		var createdAt sqliteTime
		var expiresAt nullSqliteTime
		if err := rows.Scan(
			&idStr, &tenantStr, &agentStr, &ep.UserID, &ep.SessionKey,
			&ep.Summary, &topicsBytes, &ep.TurnCount, &ep.TokenCount,
			&ep.L0Abstract, &ep.SourceID, &ep.SourceType,
			&createdAt, &expiresAt); err != nil {
			return nil, err
		}
		ep.ID, _ = uuid.Parse(idStr)
		ep.TenantID, _ = uuid.Parse(tenantStr)
		ep.AgentID, _ = uuid.Parse(agentStr)
		scanJSONStringArray(topicsBytes, &ep.KeyTopics)
		ep.CreatedAt = createdAt.Time
		if expiresAt.Valid {
			t := expiresAt.Time
			ep.ExpiresAt = &t
		}
		results = append(results, ep)
	}
	return results, rows.Err()
}

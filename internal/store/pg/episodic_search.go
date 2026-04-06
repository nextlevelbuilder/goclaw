package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// tenantFromCtx extracts tenant_id from context, returns uuid.Nil if not set.
func tenantFromCtx(ctx context.Context) uuid.UUID { return store.TenantIDFromContext(ctx) }

// episodicScored holds a search result with its individual score.
type episodicScored struct {
	id         string
	sessionKey string
	l0         string
	score      float64
	createdAt  time.Time
}

// ftsSearch performs full-text search on episodic summaries.
func (s *PGEpisodicStore) ftsSearch(ctx context.Context, query, agentID, userID string, limit int) []episodicScored {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_key, l0_abstract, ts_rank(tsv, plainto_tsquery('simple', $1)) AS score, created_at
		FROM episodic_summaries
		WHERE agent_id = $2 AND user_id = $3 AND tenant_id = $5
		  AND tsv @@ plainto_tsquery('simple', $1)
		ORDER BY score DESC LIMIT $4`,
		query, agentID, userID, limit, tenantFromCtx(ctx))
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []episodicScored
	for rows.Next() {
		var r episodicScored
		if err := rows.Scan(&r.id, &r.sessionKey, &r.l0, &r.score, &r.createdAt); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results
}

// vectorSearch performs cosine similarity search on episodic embeddings.
func (s *PGEpisodicStore) vectorSearch(ctx context.Context, embedding []float32, agentID, userID string, limit int) []episodicScored {
	vecStr := vectorToString(embedding)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_key, l0_abstract, 1 - (embedding <=> $1) AS score, created_at
		FROM episodic_summaries
		WHERE agent_id = $2 AND user_id = $3 AND tenant_id = $5
		  AND embedding IS NOT NULL
		ORDER BY embedding <=> $1 LIMIT $4`,
		vecStr, agentID, userID, limit, tenantFromCtx(ctx))
	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []episodicScored
	for rows.Next() {
		var r episodicScored
		if err := rows.Scan(&r.id, &r.sessionKey, &r.l0, &r.score, &r.createdAt); err != nil {
			continue
		}
		results = append(results, r)
	}
	return results
}

// mergeEpisodicScores merges FTS and vector results by combined weighted score.
func mergeEpisodicScores(fts, vec []episodicScored, textWeight, vecWeight float64) []episodicScored {
	byID := make(map[string]*episodicScored)
	for _, r := range fts {
		byID[r.id] = &episodicScored{id: r.id, sessionKey: r.sessionKey, l0: r.l0, createdAt: r.createdAt, score: r.score * textWeight}
	}
	for _, r := range vec {
		if existing, ok := byID[r.id]; ok {
			existing.score += r.score * vecWeight
		} else {
			byID[r.id] = &episodicScored{id: r.id, sessionKey: r.sessionKey, l0: r.l0, createdAt: r.createdAt, score: r.score * vecWeight}
		}
	}
	var merged []episodicScored
	for _, r := range byID {
		merged = append(merged, *r)
	}
	return merged
}

// scanEpisodic scans a single row into EpisodicSummary.
func scanEpisodic(row *sql.Row) (*store.EpisodicSummary, error) {
	var ep store.EpisodicSummary
	var topics json.RawMessage
	err := row.Scan(&ep.ID, &ep.TenantID, &ep.AgentID, &ep.UserID, &ep.SessionKey,
		&ep.Summary, &topics, &ep.TurnCount, &ep.TokenCount,
		&ep.L0Abstract, &ep.SourceID, &ep.SourceType, &ep.CreatedAt, &ep.ExpiresAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(topics, &ep.KeyTopics)
	return &ep, nil
}

// scanEpisodicRow scans from sql.Rows (same fields as scanEpisodic).
func scanEpisodicRow(rows *sql.Rows) (*store.EpisodicSummary, error) {
	var ep store.EpisodicSummary
	var topics json.RawMessage
	err := rows.Scan(&ep.ID, &ep.TenantID, &ep.AgentID, &ep.UserID, &ep.SessionKey,
		&ep.Summary, &topics, &ep.TurnCount, &ep.TokenCount,
		&ep.L0Abstract, &ep.SourceID, &ep.SourceType, &ep.CreatedAt, &ep.ExpiresAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(topics, &ep.KeyTopics)
	return &ep, nil
}

// Ensure PGEpisodicStore implements store.EpisodicStore.
var _ store.EpisodicStore = (*PGEpisodicStore)(nil)


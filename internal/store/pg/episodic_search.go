package pg

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
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
// Uses the stored search_vector column (GIN-indexed, 'english' config from migration 040).
// When userID is empty, returns results across all users (admin view).
func (s *PGEpisodicStore) ftsSearch(ctx context.Context, query, agentID, userID string, limit int) []episodicScored {
	q := `SELECT id, session_key, l0_abstract,
	        ts_rank(search_vector, plainto_tsquery('english', $1)) AS score, created_at
		FROM episodic_summaries
		WHERE agent_id = $2
		  AND search_vector @@ plainto_tsquery('english', $1)`
	args := []any{query, agentID}
	p := 3

	if userID != "" {
		q += fmt.Sprintf(" AND user_id = $%d", p)
		args = append(args, userID)
		p++
	}
	q += fmt.Sprintf(" AND tenant_id = $%d", p)
	args = append(args, tenantFromCtx(ctx))
	p++
	q += fmt.Sprintf(" ORDER BY score DESC LIMIT $%d", p)
	args = append(args, limit)

	var rows []episodicScoredRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil
	}
	results := make([]episodicScored, len(rows))
	for i := range rows {
		results[i] = rows[i].toEpisodicScored()
	}
	return results
}

// vectorSearch performs cosine similarity search on episodic embeddings.
// When userID is empty, returns results across all users (admin view).
func (s *PGEpisodicStore) vectorSearch(ctx context.Context, embedding []float32, agentID, userID string, limit int) []episodicScored {
	vecStr := vectorToString(embedding)
	q := `SELECT id, session_key, l0_abstract, 1 - (embedding <=> $1) AS score, created_at
		FROM episodic_summaries
		WHERE agent_id = $2
		  AND embedding IS NOT NULL`
	args := []any{vecStr, agentID}
	p := 3

	if userID != "" {
		q += fmt.Sprintf(" AND user_id = $%d", p)
		args = append(args, userID)
		p++
	}
	q += fmt.Sprintf(" AND tenant_id = $%d", p)
	args = append(args, tenantFromCtx(ctx))
	p++
	q += fmt.Sprintf(" ORDER BY embedding <=> $1 LIMIT $%d", p)
	args = append(args, limit)

	var rows []episodicScoredRow
	if err := pkgSqlxDB.SelectContext(ctx, &rows, q, args...); err != nil {
		return nil
	}
	results := make([]episodicScored, len(rows))
	for i := range rows {
		results[i] = rows[i].toEpisodicScored()
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

// scanEpisodic scans a single row into EpisodicSummary. Column order matches
// the SELECT list in PGEpisodicStore.Get (17 columns incl. recall signals).
func scanEpisodic(row *sql.Row) (*store.EpisodicSummary, error) {
	var ep store.EpisodicSummary
	var topics pq.StringArray
	err := row.Scan(&ep.ID, &ep.TenantID, &ep.AgentID, &ep.UserID, &ep.SessionKey,
		&ep.Summary, &topics, &ep.TurnCount, &ep.TokenCount,
		&ep.L0Abstract, &ep.SourceID, &ep.SourceType, &ep.CreatedAt, &ep.ExpiresAt,
		&ep.RecallCount, &ep.RecallScore, &ep.LastRecalledAt)
	if err != nil {
		return nil, err
	}
	ep.KeyTopics = []string(topics)
	return &ep, nil
}

// Ensure PGEpisodicStore implements store.EpisodicStore.
var _ store.EpisodicStore = (*PGEpisodicStore)(nil)

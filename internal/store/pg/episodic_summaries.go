package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGEpisodicStore implements store.EpisodicStore backed by PostgreSQL.
type PGEpisodicStore struct {
	db          *sql.DB
	embProvider store.EmbeddingProvider
}

// NewPGEpisodicStore creates a new PG-backed episodic store.
func NewPGEpisodicStore(db *sql.DB) *PGEpisodicStore {
	return &PGEpisodicStore{db: db}
}

func (s *PGEpisodicStore) SetEmbeddingProvider(p store.EmbeddingProvider) { s.embProvider = p }
func (s *PGEpisodicStore) Close() error                                  { return nil }

// Create inserts a new episodic summary with optional embedding.
func (s *PGEpisodicStore) Create(ctx context.Context, ep *store.EpisodicSummary) error {
	id := uuid.Must(uuid.NewV7())
	ep.ID = id

	topics, _ := json.Marshal(ep.KeyTopics)
	now := time.Now().UTC()

	var embStr *string
	if s.embProvider != nil && ep.Summary != "" {
		vecs, err := s.embProvider.Embed(ctx, []string{ep.Summary})
		if err == nil && len(vecs) > 0 {
			v := vectorToString(vecs[0])
			embStr = &v
		} else if err != nil {
			slog.Warn("episodic: embedding failed", "err", err)
		}
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO episodic_summaries
			(id, tenant_id, agent_id, user_id, session_key, summary, key_topics,
			 turn_count, token_count, embedding, l0_abstract, source_id,
			 source_type, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		ON CONFLICT (agent_id, user_id, source_id) DO NOTHING`,
		id, ep.TenantID, ep.AgentID, ep.UserID, ep.SessionKey,
		ep.Summary, topics, ep.TurnCount, ep.TokenCount,
		embStr, ep.L0Abstract, ep.SourceID, ep.SourceType, now, ep.ExpiresAt)
	if err != nil {
		return fmt.Errorf("episodic create: %w", err)
	}
	ep.CreatedAt = now
	return nil
}

// Get retrieves an episodic summary by ID.
func (s *PGEpisodicStore) Get(ctx context.Context, id string) (*store.EpisodicSummary, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, agent_id, user_id, session_key, summary, key_topics,
		       turn_count, token_count, l0_abstract, source_id, source_type,
		       created_at, expires_at
		FROM episodic_summaries WHERE id = $1 AND tenant_id = $2`,
		id, store.TenantIDFromContext(ctx))
	return scanEpisodic(row)
}

// Delete removes an episodic summary.
func (s *PGEpisodicStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM episodic_summaries WHERE id = $1 AND tenant_id = $2`,
		id, store.TenantIDFromContext(ctx))
	return err
}

// List returns episodic summaries ordered by created_at DESC.
// When userID is empty, returns summaries for all users of the agent.
func (s *PGEpisodicStore) List(ctx context.Context, agentID, userID string, limit, offset int) ([]store.EpisodicSummary, error) {
	if limit <= 0 {
		limit = 20
	}
	tenantID := store.TenantIDFromContext(ctx)

	var rows *sql.Rows
	var err error
	if userID != "" {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, tenant_id, agent_id, user_id, session_key, summary, key_topics,
			       turn_count, token_count, l0_abstract, source_id, source_type,
			       created_at, expires_at
			FROM episodic_summaries
			WHERE agent_id = $1 AND user_id = $2 AND tenant_id = $5
			ORDER BY created_at DESC LIMIT $3 OFFSET $4`,
			agentID, userID, limit, offset, tenantID)
	} else {
		rows, err = s.db.QueryContext(ctx, `
			SELECT id, tenant_id, agent_id, user_id, session_key, summary, key_topics,
			       turn_count, token_count, l0_abstract, source_id, source_type,
			       created_at, expires_at
			FROM episodic_summaries
			WHERE agent_id = $1 AND tenant_id = $4
			ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
			agentID, limit, offset, tenantID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []store.EpisodicSummary
	for rows.Next() {
		ep, err := scanEpisodicRow(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, *ep)
	}
	return results, rows.Err()
}

// Search performs hybrid FTS + vector search on episodic summaries.
func (s *PGEpisodicStore) Search(ctx context.Context, query, agentID, userID string, opts store.EpisodicSearchOptions) ([]store.EpisodicSearchResult, error) {
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}
	vw := opts.VectorWeight
	if vw == 0 {
		vw = 0.6
	}
	tw := opts.TextWeight
	if tw == 0 {
		tw = 0.4
	}

	// FTS search
	ftsResults := s.ftsSearch(ctx, query, agentID, userID, maxResults*2)

	// Vector search (if embedding provider available)
	var vecResults []episodicScored
	if s.embProvider != nil {
		vecs, err := s.embProvider.Embed(ctx, []string{query})
		if err == nil && len(vecs) > 0 {
			vecResults = s.vectorSearch(ctx, vecs[0], agentID, userID, maxResults*2)
		}
	}

	// Merge by combined score
	merged := mergeEpisodicScores(ftsResults, vecResults, tw, vw)
	sort.Slice(merged, func(i, j int) bool { return merged[i].score > merged[j].score })

	if len(merged) > maxResults {
		merged = merged[:maxResults]
	}

	var results []store.EpisodicSearchResult
	for _, m := range merged {
		if opts.MinScore > 0 && m.score < opts.MinScore {
			continue
		}
		results = append(results, store.EpisodicSearchResult{
			EpisodicID: m.id, L0Abstract: m.l0, Score: m.score,
			CreatedAt: m.createdAt, SessionKey: m.sessionKey,
		})
	}
	return results, nil
}

// ExistsBySourceID checks if an episodic summary with the given source_id exists (idempotency).
func (s *PGEpisodicStore) ExistsBySourceID(ctx context.Context, agentID, userID, sourceID string) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx, `
		SELECT EXISTS(SELECT 1 FROM episodic_summaries
		WHERE agent_id = $1 AND user_id = $2 AND source_id = $3 AND tenant_id = $4)`,
		agentID, userID, sourceID, store.TenantIDFromContext(ctx)).Scan(&exists)
	return exists, err
}

// PruneExpired deletes episodic summaries past their expiry within the caller's tenant.
func (s *PGEpisodicStore) PruneExpired(ctx context.Context) (int, error) {
	tenantID := store.TenantIDFromContext(ctx)
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM episodic_summaries
		WHERE expires_at IS NOT NULL AND expires_at < NOW() AND tenant_id = $1`, tenantID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

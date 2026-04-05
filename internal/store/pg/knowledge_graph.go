package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// PGKnowledgeGraphStore implements store.KnowledgeGraphStore backed by Postgres.
type PGKnowledgeGraphStore struct {
	db          *sql.DB
	embProvider store.EmbeddingProvider
}

// NewPGKnowledgeGraphStore creates a new PG-backed knowledge graph store.
func NewPGKnowledgeGraphStore(db *sql.DB) *PGKnowledgeGraphStore {
	return &PGKnowledgeGraphStore{db: db}
}

// SetEmbeddingProvider configures the embedding provider for semantic search.
func (s *PGKnowledgeGraphStore) SetEmbeddingProvider(provider store.EmbeddingProvider) {
	s.embProvider = provider
}

func (s *PGKnowledgeGraphStore) UpsertEntity(ctx context.Context, entity *store.Entity) error {
	aid := mustParseUUID(entity.AgentID)
	props, err := json.Marshal(entity.Properties)
	if err != nil {
		props = []byte("{}")
	}
	now := time.Now()
	id := uuid.Must(uuid.NewV7())
	tid := tenantIDForInsert(ctx)
	var actualID uuid.UUID
	if err = s.db.QueryRowContext(ctx, `
		INSERT INTO kg_entities
			(id, agent_id, user_id, external_id, name, entity_type, description, properties, source_id, confidence, tenant_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12)
		ON CONFLICT (agent_id, user_id, external_id) DO UPDATE SET
			name        = EXCLUDED.name,
			entity_type = EXCLUDED.entity_type,
			description = EXCLUDED.description,
			properties  = EXCLUDED.properties,
			source_id   = EXCLUDED.source_id,
			confidence  = EXCLUDED.confidence,
			tenant_id   = EXCLUDED.tenant_id,
			updated_at  = EXCLUDED.updated_at
		RETURNING id`,
		id, aid, entity.UserID, entity.ExternalID, entity.Name, entity.EntityType,
		entity.Description, props, entity.SourceID, entity.Confidence, tid, now,
	).Scan(&actualID); err != nil {
		return err
	}

	// Generate embedding in background (best-effort, non-blocking)
	go s.EmbedEntity(context.WithoutCancel(ctx), actualID.String(), entity.Name, entity.Description)
	return nil
}

func (s *PGKnowledgeGraphStore) GetEntity(ctx context.Context, agentID, userID, entityID string) (*store.Entity, error) {
	aid := mustParseUUID(agentID)
	eid := mustParseUUID(entityID)

	if store.IsSharedKG(ctx) {
		tc, tcArgs, _, err := scopeClause(ctx, 3)
		if err != nil {
			return nil, err
		}
		row := s.db.QueryRowContext(ctx, `
			SELECT id, agent_id, user_id, external_id, name, entity_type, description,
			       properties, source_id, confidence, created_at, updated_at
			FROM kg_entities WHERE id = $1 AND agent_id = $2`+tc,
			append([]any{eid, aid}, tcArgs...)...,
		)
		return scanEntity(row)
	}

	tc, tcArgs, _, err := scopeClause(ctx, 4)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRowContext(ctx, `
		SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		       properties, source_id, confidence, created_at, updated_at
		FROM kg_entities WHERE id = $1 AND agent_id = $2 AND user_id = $3`+tc,
		append([]any{eid, aid, userID}, tcArgs...)...,
	)
	return scanEntity(row)
}

func (s *PGKnowledgeGraphStore) DeleteEntity(ctx context.Context, agentID, userID, entityID string) error {
	aid := mustParseUUID(agentID)
	eid := mustParseUUID(entityID)
	if store.IsSharedKG(ctx) {
		tc, tcArgs, _, err := scopeClause(ctx, 3)
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx,
			`DELETE FROM kg_entities WHERE id = $1 AND agent_id = $2`+tc,
			append([]any{eid, aid}, tcArgs...)...,
		)
		return err
	}
	tc, tcArgs, _, err := scopeClause(ctx, 4)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM kg_entities WHERE id = $1 AND agent_id = $2 AND user_id = $3`+tc,
		append([]any{eid, aid, userID}, tcArgs...)...,
	)
	return err
}

func (s *PGKnowledgeGraphStore) ListEntities(ctx context.Context, agentID, userID string, opts store.EntityListOptions) ([]store.Entity, error) {
	aid := mustParseUUID(agentID)
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	// Build dynamic WHERE clause: always filter by agent_id, optionally by user_id and entity_type
	where := "agent_id = $1"
	args := []any{aid}
	idx := 2
	if !store.IsSharedKG(ctx) && userID != "" {
		where += fmt.Sprintf(" AND user_id = $%d", idx)
		args = append(args, userID)
		idx++
	}
	if opts.EntityType != "" {
		where += fmt.Sprintf(" AND entity_type = $%d", idx)
		args = append(args, opts.EntityType)
		idx++
	}
	tc, tcArgs, _, err := scopeClause(ctx, idx)
	if err != nil {
		return nil, err
	}
	if tc != "" {
		where += tc
		args = append(args, tcArgs...)
		idx++
	}
	args = append(args, limit, opts.Offset)
	query := fmt.Sprintf(`
		SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		       properties, source_id, confidence, created_at, updated_at
		FROM kg_entities WHERE %s
		ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, where, idx, idx+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntities(rows)
}

func (s *PGKnowledgeGraphStore) SearchEntities(ctx context.Context, agentID, userID, query string, limit int) ([]store.Entity, error) {
	aid := mustParseUUID(agentID)
	if limit <= 0 {
		limit = 20
	}

	shared := store.IsSharedKG(ctx)

	// FTS search using tsvector
	ftsResults, err := s.ftsSearchEntities(ctx, aid, userID, query, limit*2, shared)
	if err != nil {
		return nil, err
	}

	// Vector search if provider available
	var vecResults []scoredEntity
	if s.embProvider != nil {
		embeddings, embErr := s.embProvider.Embed(ctx, []string{query})
		if embErr == nil && len(embeddings) > 0 {
			vecResults, err = s.vectorSearchEntities(ctx, embeddings[0], aid, userID, limit*2, shared)
			if err != nil {
				vecResults = nil
			}
		}
	}

	// If no vector results, fall back to FTS-only
	if len(vecResults) == 0 {
		if len(ftsResults) > limit {
			ftsResults = ftsResults[:limit]
		}
		entities := make([]store.Entity, len(ftsResults))
		for i, r := range ftsResults {
			entities[i] = r.Entity
		}
		return entities, nil
	}

	// Hybrid merge with weights: 0.3 FTS, 0.7 vector
	textW, vecW := 0.3, 0.7
	if len(ftsResults) == 0 {
		textW, vecW = 0, 1.0
	}
	merged := hybridMergeEntities(ftsResults, vecResults, textW, vecW)

	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

type scoredEntity struct {
	Entity store.Entity
	Score  float64
}

func (s *PGKnowledgeGraphStore) ftsSearchEntities(ctx context.Context, agentID uuid.UUID, userID, query string, limit int, shared bool) ([]scoredEntity, error) {
	where := "agent_id = $1 AND tsv @@ plainto_tsquery('simple', $2)"
	args := []any{agentID, query}
	idx := 3
	if !shared && userID != "" {
		where += fmt.Sprintf(" AND user_id = $%d", idx)
		args = append(args, userID)
		idx++
	}
	tc, tcArgs, _, err := scopeClause(ctx, idx)
	if err != nil {
		return nil, err
	}
	if tc != "" {
		where += tc
		args = append(args, tcArgs...)
		idx++
	}
	args = append(args, query, limit)
	q := fmt.Sprintf(`
		SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		       properties, source_id, confidence, created_at, updated_at,
		       ts_rank(tsv, plainto_tsquery('simple', $%d)) AS score
		FROM kg_entities
		WHERE %s
		ORDER BY score DESC LIMIT $%d`, idx, where, idx+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []scoredEntity
	for rows.Next() {
		var e store.Entity
		var props []byte
		var createdAt, updatedAt time.Time
		var score float64
		if err := rows.Scan(
			&e.ID, &e.AgentID, &e.UserID, &e.ExternalID, &e.Name, &e.EntityType,
			&e.Description, &props, &e.SourceID, &e.Confidence, &createdAt, &updatedAt,
			&score,
		); err != nil {
			continue
		}
		json.Unmarshal(props, &e.Properties) //nolint:errcheck
		e.CreatedAt = createdAt.UnixMilli()
		e.UpdatedAt = updatedAt.UnixMilli()
		results = append(results, scoredEntity{Entity: e, Score: score})
	}
	return results, rows.Err()
}

func (s *PGKnowledgeGraphStore) vectorSearchEntities(ctx context.Context, embedding []float32, agentID uuid.UUID, userID string, limit int, shared bool) ([]scoredEntity, error) {
	vecStr := vectorToString(embedding)

	where := "agent_id = $1 AND embedding IS NOT NULL"
	args := []any{agentID}
	idx := 2
	if !shared && userID != "" {
		where += fmt.Sprintf(" AND user_id = $%d", idx)
		args = append(args, userID)
		idx++
	}
	tc, tcArgs, _, err := scopeClause(ctx, idx)
	if err != nil {
		return nil, err
	}
	if tc != "" {
		where += tc
		args = append(args, tcArgs...)
		idx++
	}
	args = append(args, vecStr, limit)
	q := fmt.Sprintf(`
		SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		       properties, source_id, confidence, created_at, updated_at,
		       1 - (embedding <=> $%d::vector) AS score
		FROM kg_entities
		WHERE %s
		ORDER BY embedding <=> $%d::vector LIMIT $%d`, idx, where, idx, idx+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []scoredEntity
	for rows.Next() {
		var e store.Entity
		var props []byte
		var createdAt, updatedAt time.Time
		var score float64
		if err := rows.Scan(
			&e.ID, &e.AgentID, &e.UserID, &e.ExternalID, &e.Name, &e.EntityType,
			&e.Description, &props, &e.SourceID, &e.Confidence, &createdAt, &updatedAt,
			&score,
		); err != nil {
			continue
		}
		json.Unmarshal(props, &e.Properties) //nolint:errcheck
		e.CreatedAt = createdAt.UnixMilli()
		e.UpdatedAt = updatedAt.UnixMilli()
		results = append(results, scoredEntity{Entity: e, Score: score})
	}
	return results, rows.Err()
}

// hybridMergeEntities combines ILIKE and vector results with weighted scoring.
func hybridMergeEntities(ilike, vec []scoredEntity, textWeight, vectorWeight float64) []store.Entity {
	type mergedEntry struct {
		Entity store.Entity
		Score  float64
	}
	seen := make(map[string]*mergedEntry)

	for _, r := range ilike {
		if existing, ok := seen[r.Entity.ID]; ok {
			existing.Score += r.Score * textWeight
		} else {
			seen[r.Entity.ID] = &mergedEntry{Entity: r.Entity, Score: r.Score * textWeight}
		}
	}
	for _, r := range vec {
		if existing, ok := seen[r.Entity.ID]; ok {
			existing.Score += r.Score * vectorWeight
		} else {
			seen[r.Entity.ID] = &mergedEntry{Entity: r.Entity, Score: r.Score * vectorWeight}
		}
	}

	results := make([]store.Entity, 0, len(seen))
	scores := make(map[string]float64, len(seen))
	for id, entry := range seen {
		results = append(results, entry.Entity)
		scores[id] = entry.Score
	}

	slices.SortFunc(results, func(a, b store.Entity) int {
		return cmp.Compare(scores[b.ID], scores[a.ID]) // descending
	})

	return results
}

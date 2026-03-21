package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"cmp"
	"fmt"
	"log/slog"
	"slices"
	"strings"
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
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO kg_entities
			(id, agent_id, user_id, external_id, name, entity_type, description, properties, source_id, confidence, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
		ON CONFLICT (agent_id, user_id, external_id) DO UPDATE SET
			name        = EXCLUDED.name,
			entity_type = EXCLUDED.entity_type,
			description = EXCLUDED.description,
			properties  = EXCLUDED.properties,
			source_id   = EXCLUDED.source_id,
			confidence  = EXCLUDED.confidence,
			updated_at  = EXCLUDED.updated_at`,
		id, aid, entity.UserID, entity.ExternalID, entity.Name, entity.EntityType,
		entity.Description, props, entity.SourceID, entity.Confidence, now,
	)
	return err
}

func (s *PGKnowledgeGraphStore) GetEntity(ctx context.Context, agentID, userID, entityID string) (*store.Entity, error) {
	aid := mustParseUUID(agentID)
	eid := mustParseUUID(entityID)

	if store.IsSharedKG(ctx) {
		row := s.db.QueryRowContext(ctx, `
			SELECT id, agent_id, user_id, external_id, name, entity_type, description,
			       properties, source_id, confidence, created_at, updated_at
			FROM kg_entities WHERE id = $1 AND agent_id = $2`,
			eid, aid,
		)
		return scanEntity(row)
	}

	row := s.db.QueryRowContext(ctx, `
		SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		       properties, source_id, confidence, created_at, updated_at
		FROM kg_entities WHERE id = $1 AND agent_id = $2 AND user_id = $3`,
		eid, aid, userID,
	)
	return scanEntity(row)
}

func (s *PGKnowledgeGraphStore) DeleteEntity(ctx context.Context, agentID, userID, entityID string) error {
	aid := mustParseUUID(agentID)
	eid := mustParseUUID(entityID)
	if store.IsSharedKG(ctx) {
		_, err := s.db.ExecContext(ctx,
			`DELETE FROM kg_entities WHERE id = $1 AND agent_id = $2`,
			eid, aid,
		)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM kg_entities WHERE id = $1 AND agent_id = $2 AND user_id = $3`,
		eid, aid, userID,
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

	// ILIKE search
	ilikeResults, err := s.ilikeSearchEntities(ctx, aid, userID, query, limit*2, shared)
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

	// If no vector results, fall back to ILIKE-only
	if len(vecResults) == 0 {
		if len(ilikeResults) > limit {
			ilikeResults = ilikeResults[:limit]
		}
		entities := make([]store.Entity, len(ilikeResults))
		for i, r := range ilikeResults {
			entities[i] = r.Entity
		}
		return entities, nil
	}

	// Hybrid merge with weights: 0.3 ILIKE, 0.7 vector
	textW, vecW := 0.3, 0.7
	if len(ilikeResults) == 0 {
		textW, vecW = 0, 1.0
	}
	merged := hybridMergeEntities(ilikeResults, vecResults, textW, vecW)

	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

type scoredEntity struct {
	Entity store.Entity
	Score  float64
}

func (s *PGKnowledgeGraphStore) ilikeSearchEntities(ctx context.Context, agentID uuid.UUID, userID, query string, limit int, shared bool) ([]scoredEntity, error) {
	escaped := strings.NewReplacer("%", "\\%", "_", "\\_").Replace(query)
	pattern := "%" + escaped + "%"

	where := "agent_id = $1"
	args := []any{agentID}
	idx := 2
	if !shared && userID != "" {
		where += fmt.Sprintf(" AND user_id = $%d", idx)
		args = append(args, userID)
		idx++
	}
	args = append(args, pattern, limit)
	q := fmt.Sprintf(`
		SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		       properties, source_id, confidence, created_at, updated_at
		FROM kg_entities
		WHERE %s AND (name ILIKE $%d ESCAPE '\' OR description ILIKE $%d ESCAPE '\')
		ORDER BY confidence DESC, updated_at DESC LIMIT $%d`, where, idx, idx, idx+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []scoredEntity
	rank := 1.0
	for rows.Next() {
		var e store.Entity
		var props []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(
			&e.ID, &e.AgentID, &e.UserID, &e.ExternalID, &e.Name, &e.EntityType,
			&e.Description, &props, &e.SourceID, &e.Confidence, &createdAt, &updatedAt,
		); err != nil {
			continue
		}
		json.Unmarshal(props, &e.Properties) //nolint:errcheck
		e.CreatedAt = createdAt.UnixMilli()
		e.UpdatedAt = updatedAt.UnixMilli()
		results = append(results, scoredEntity{Entity: e, Score: rank})
		rank *= 0.95 // decay for rank-based scoring
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

func (s *PGKnowledgeGraphStore) UpsertRelation(ctx context.Context, relation *store.Relation) error {
	aid := mustParseUUID(relation.AgentID)
	src := mustParseUUID(relation.SourceEntityID)
	tgt := mustParseUUID(relation.TargetEntityID)
	props, err := json.Marshal(relation.Properties)
	if err != nil {
		props = []byte("{}")
	}
	id := uuid.Must(uuid.NewV7())
	now := time.Now()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO kg_relations
			(id, agent_id, user_id, source_entity_id, relation_type, target_entity_id, confidence, properties, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (agent_id, user_id, source_entity_id, relation_type, target_entity_id) DO UPDATE SET
			confidence  = EXCLUDED.confidence,
			properties  = EXCLUDED.properties`,
		id, aid, relation.UserID, src, relation.RelationType, tgt, relation.Confidence, props, now,
	)
	return err
}

func (s *PGKnowledgeGraphStore) DeleteRelation(ctx context.Context, agentID, userID, relationID string) error {
	aid := mustParseUUID(agentID)
	rid := mustParseUUID(relationID)
	if store.IsSharedKG(ctx) {
		_, err := s.db.ExecContext(ctx,
			`DELETE FROM kg_relations WHERE id = $1 AND agent_id = $2`,
			rid, aid,
		)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM kg_relations WHERE id = $1 AND agent_id = $2 AND user_id = $3`,
		rid, aid, userID,
	)
	return err
}

func (s *PGKnowledgeGraphStore) ListRelations(ctx context.Context, agentID, userID, entityID string) ([]store.Relation, error) {
	aid := mustParseUUID(agentID)
	eid := mustParseUUID(entityID)

	var q string
	var args []any
	if store.IsSharedKG(ctx) {
		q = `SELECT id, agent_id, user_id, source_entity_id, relation_type, target_entity_id,
		       confidence, properties, created_at
		FROM kg_relations
		WHERE agent_id = $1
		  AND (source_entity_id = $2 OR target_entity_id = $2)
		ORDER BY created_at DESC`
		args = []any{aid, eid}
	} else {
		q = `SELECT id, agent_id, user_id, source_entity_id, relation_type, target_entity_id,
		       confidence, properties, created_at
		FROM kg_relations
		WHERE agent_id = $1 AND user_id = $2
		  AND (source_entity_id = $3 OR target_entity_id = $3)
		ORDER BY created_at DESC`
		args = []any{aid, userID, eid}
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRelations(rows)
}

func (s *PGKnowledgeGraphStore) ListAllRelations(ctx context.Context, agentID, userID string, limit int) ([]store.Relation, error) {
	aid := mustParseUUID(agentID)
	if limit <= 0 {
		limit = 200
	}
	where := "agent_id = $1"
	args := []any{aid}
	idx := 2
	if !store.IsSharedKG(ctx) && userID != "" {
		where += fmt.Sprintf(" AND user_id = $%d", idx)
		args = append(args, userID)
		idx++
	}
	args = append(args, limit)
	q := fmt.Sprintf(`
		SELECT id, agent_id, user_id, source_entity_id, relation_type, target_entity_id,
		       confidence, properties, created_at
		FROM kg_relations WHERE %s
		ORDER BY created_at DESC LIMIT $%d`, where, idx)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRelations(rows)
}

func (s *PGKnowledgeGraphStore) IngestExtraction(ctx context.Context, agentID, userID string, entities []store.Entity, relations []store.Relation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	aid := mustParseUUID(agentID)
	now := time.Now()

	// Upsert entities and build external_id → DB UUID lookup for relations
	extIDToUUID := make(map[string]uuid.UUID, len(entities))
	for i := range entities {
		e := &entities[i]
		e.AgentID = agentID
		e.UserID = userID
		props, _ := json.Marshal(e.Properties)
		id := uuid.Must(uuid.NewV7())
		// Use RETURNING to get the actual ID (could be existing row on conflict)
		var actualID uuid.UUID
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO kg_entities
				(id, agent_id, user_id, external_id, name, entity_type, description, properties, source_id, confidence, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $11)
			ON CONFLICT (agent_id, user_id, external_id) DO UPDATE SET
				name        = EXCLUDED.name,
				entity_type = EXCLUDED.entity_type,
				description = EXCLUDED.description,
				properties  = EXCLUDED.properties,
				source_id   = EXCLUDED.source_id,
				confidence  = EXCLUDED.confidence,
				updated_at  = EXCLUDED.updated_at
			RETURNING id`,
			id, aid, userID, e.ExternalID, e.Name, e.EntityType,
			e.Description, props, e.SourceID, e.Confidence, now,
		).Scan(&actualID); err != nil {
			return err
		}
		extIDToUUID[e.ExternalID] = actualID
	}

	// Batch-generate embeddings for all upserted entities (fire-and-forget on error).
	if s.embProvider != nil && len(extIDToUUID) > 0 {
		texts := make([]string, 0, len(entities))
		ids := make([]uuid.UUID, 0, len(entities))
		for _, e := range entities {
			texts = append(texts, e.Name+" "+e.Description)
			ids = append(ids, extIDToUUID[e.ExternalID])
		}
		embeddings, embErr := s.embProvider.Embed(ctx, texts)
		if embErr != nil {
			slog.Warn("kg entity embedding batch failed", "error", embErr)
		} else {
			for i, emb := range embeddings {
				if len(emb) == 0 {
					continue
				}
				vecStr := vectorToString(emb)
				if _, err := tx.ExecContext(ctx,
					`UPDATE kg_entities SET embedding = $1::vector WHERE id = $2`,
					vecStr, ids[i],
				); err != nil {
					slog.Warn("kg entity embedding update failed", "entity_id", ids[i], "error", err)
				}
			}
		}
	}

	for i := range relations {
		r := &relations[i]
		r.AgentID = agentID
		r.UserID = userID
		// Resolve external_id references to actual DB UUIDs
		src, ok1 := extIDToUUID[r.SourceEntityID]
		tgt, ok2 := extIDToUUID[r.TargetEntityID]
		if !ok1 || !ok2 {
			continue // skip relations referencing unknown entities
		}
		props, _ := json.Marshal(r.Properties)
		id := uuid.Must(uuid.NewV7())
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO kg_relations
				(id, agent_id, user_id, source_entity_id, relation_type, target_entity_id, confidence, properties, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT (agent_id, user_id, source_entity_id, relation_type, target_entity_id) DO UPDATE SET
				confidence  = EXCLUDED.confidence,
				properties  = EXCLUDED.properties`,
			id, aid, userID, src, r.RelationType, tgt, r.Confidence, props, now,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *PGKnowledgeGraphStore) PruneByConfidence(ctx context.Context, agentID, userID string, minConfidence float64) (int, error) {
	aid := mustParseUUID(agentID)
	var res sql.Result
	var err error
	if store.IsSharedKG(ctx) {
		res, err = s.db.ExecContext(ctx,
			`DELETE FROM kg_entities WHERE agent_id = $1 AND confidence < $2`,
			aid, minConfidence,
		)
	} else {
		res, err = s.db.ExecContext(ctx,
			`DELETE FROM kg_entities WHERE agent_id = $1 AND user_id = $2 AND confidence < $3`,
			aid, userID, minConfidence,
		)
	}
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *PGKnowledgeGraphStore) Stats(ctx context.Context, agentID, userID string) (*store.GraphStats, error) {
	aid := mustParseUUID(agentID)
	stats := &store.GraphStats{EntityTypes: make(map[string]int)}

	userFilter := ""
	args := []any{aid}
	if userID != "" {
		userFilter = " AND user_id = $2"
		args = append(args, userID)
	}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kg_entities WHERE agent_id = $1`+userFilter, args...,
	).Scan(&stats.EntityCount); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kg_relations WHERE agent_id = $1`+userFilter, args...,
	).Scan(&stats.RelationCount); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT entity_type, COUNT(*) FROM kg_entities WHERE agent_id = $1`+userFilter+` GROUP BY entity_type`, args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t string
		var c int
		if err := rows.Scan(&t, &c); err != nil {
			continue
		}
		stats.EntityTypes[t] = c
	}
	return stats, nil
}

func (s *PGKnowledgeGraphStore) Close() error { return nil }

// --- scan helpers ---

type rowScanner interface {
	Scan(dest ...any) error
}

func scanEntity(row rowScanner) (*store.Entity, error) {
	var e store.Entity
	var props []byte
	var createdAt, updatedAt time.Time
	if err := row.Scan(
		&e.ID, &e.AgentID, &e.UserID, &e.ExternalID, &e.Name, &e.EntityType,
		&e.Description, &props, &e.SourceID, &e.Confidence, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	json.Unmarshal(props, &e.Properties) //nolint:errcheck
	e.CreatedAt = createdAt.UnixMilli()
	e.UpdatedAt = updatedAt.UnixMilli()
	return &e, nil
}

func scanEntities(rows *sql.Rows) ([]store.Entity, error) {
	var result []store.Entity
	for rows.Next() {
		var e store.Entity
		var props []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(
			&e.ID, &e.AgentID, &e.UserID, &e.ExternalID, &e.Name, &e.EntityType,
			&e.Description, &props, &e.SourceID, &e.Confidence, &createdAt, &updatedAt,
		); err != nil {
			continue
		}
		json.Unmarshal(props, &e.Properties) //nolint:errcheck
		e.CreatedAt = createdAt.UnixMilli()
		e.UpdatedAt = updatedAt.UnixMilli()
		result = append(result, e)
	}
	return result, rows.Err()
}

func scanRelations(rows *sql.Rows) ([]store.Relation, error) {
	var result []store.Relation
	for rows.Next() {
		var r store.Relation
		var props []byte
		var createdAt time.Time
		if err := rows.Scan(
			&r.ID, &r.AgentID, &r.UserID, &r.SourceEntityID, &r.RelationType,
			&r.TargetEntityID, &r.Confidence, &props, &createdAt,
		); err != nil {
			continue
		}
		json.Unmarshal(props, &r.Properties) //nolint:errcheck
		r.CreatedAt = createdAt.UnixMilli()
		result = append(result, r)
	}
	return result, rows.Err()
}

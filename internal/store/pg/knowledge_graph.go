package pg

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// kgUserWhere returns a WHERE fragment and args for user scoping.
//   - specific IDs: " AND user_id = ANY($N::text[])" with ids
//   - shared mode:  "" (no filter)
//   - per-user:     " AND user_id = $N" with userID
func kgUserWhere(ctx context.Context, userID string, argIdx int) (string, []any) {
	if ids := store.SharedKGIDsFromCtx(ctx); len(ids) > 0 {
		return fmt.Sprintf(" AND user_id = ANY($%d::text[])", argIdx), []any{ids}
	}
	if store.IsSharedKG(ctx) {
		return "", nil
	}
	if userID == "" {
		return "", nil
	}
	return fmt.Sprintf(" AND user_id = $%d", argIdx), []any{userID}
}

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
	aid, err := parseUUID(entity.AgentID)
	if err != nil {
		return fmt.Errorf("kg upsert entity: %w", err)
	}
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
			(id, agent_id, user_id, external_id, name, entity_type, description, properties, source_id, confidence, tenant_id, created_at, updated_at, event_time)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $12, $13)
		ON CONFLICT (agent_id, user_id, external_id) DO UPDATE SET
			name        = EXCLUDED.name,
			entity_type = EXCLUDED.entity_type,
			description = EXCLUDED.description,
			properties  = EXCLUDED.properties,
			source_id   = EXCLUDED.source_id,
			confidence  = EXCLUDED.confidence,
			tenant_id   = EXCLUDED.tenant_id,
			updated_at  = EXCLUDED.updated_at,
			event_time  = CASE WHEN EXCLUDED.event_time IS NOT NULL THEN EXCLUDED.event_time ELSE kg_entities.event_time END
		RETURNING id`,
		id, aid, entity.UserID, entity.ExternalID, entity.Name, entity.EntityType,
		entity.Description, props, entity.SourceID, entity.Confidence, tid, now, entity.EventTime,
	).Scan(&actualID); err != nil {
		return err
	}

	// Generate embedding in background (best-effort, non-blocking)
	go s.EmbedEntity(context.WithoutCancel(ctx), actualID.String(), entity.Name, entity.Description)
	return nil
}

func (s *PGKnowledgeGraphStore) GetEntity(ctx context.Context, agentID, userID, entityID string) (*store.Entity, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("kg get entity: agent: %w", err)
	}
	eid, err := parseUUID(entityID)
	if err != nil {
		return nil, fmt.Errorf("kg get entity: id: %w", err)
	}

	userWhere, userArgs := kgUserWhere(ctx, userID, 3)
	args := append([]any{eid, aid}, userArgs...)
	tc, tcArgs, _, err := scopeClause(ctx, 3+len(userArgs))
	if err != nil {
		return nil, err
	}
	args = append(args, tcArgs...)

	var row entityRow
	err = pkgSqlxDB.GetContext(ctx, &row, `
		SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		       properties, source_id, confidence, created_at, updated_at, event_time
		FROM kg_entities WHERE id = $1 AND agent_id = $2`+userWhere+tc,
		args...,
	)
	if err != nil {
		return nil, err
	}
	e := row.toEntity()
	return &e, nil
}

func (s *PGKnowledgeGraphStore) DeleteEntity(ctx context.Context, agentID, userID, entityID string) error {
	aid, err := parseUUID(agentID)
	if err != nil {
		return fmt.Errorf("kg delete entity: agent: %w", err)
	}
	eid, err := parseUUID(entityID)
	if err != nil {
		return fmt.Errorf("kg delete entity: id: %w", err)
	}
	userWhere, userArgs := kgUserWhere(ctx, userID, 3)
	args := append([]any{eid, aid}, userArgs...)
	tc, tcArgs, _, err := scopeClause(ctx, 3+len(userArgs))
	if err != nil {
		return err
	}
	args = append(args, tcArgs...)
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM kg_entities WHERE id = $1 AND agent_id = $2`+userWhere+tc,
		args...,
	)
	return err
}

func (s *PGKnowledgeGraphStore) DeleteEntities(ctx context.Context, agentID, userID string, entityIDs []string) (int, error) {
	if len(entityIDs) == 0 {
		return 0, nil
	}
	aid, err := parseUUID(agentID)
	if err != nil {
		return 0, fmt.Errorf("kg delete entities: agent: %w", err)
	}
	pgIDs := make([]uuid.UUID, len(entityIDs))
	for i, id := range entityIDs {
		pgIDs[i], err = parseUUID(id)
		if err != nil {
			return 0, fmt.Errorf("kg delete entities: id[%d]: %w", i, err)
		}
	}

	userWhere, userArgs := kgUserWhere(ctx, userID, 3)
	tc, tcArgs, _, tcErr := scopeClause(ctx, 3+len(userArgs))
	if tcErr != nil {
		return 0, tcErr
	}
	scopeSuffix := userWhere + tc
	args := append([]any{pgIDs, aid}, userArgs...)
	args = append(args, tcArgs...)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	// Delete relations referencing these entities
	relWhere := "agent_id = $2" + scopeSuffix
	_, delErr := tx.ExecContext(ctx,
		"DELETE FROM kg_relations WHERE (source_entity_id = ANY($1) OR target_entity_id = ANY($1)) AND "+relWhere,
		args...,
	)
	if delErr != nil {
		return 0, delErr
	}
	// Delete dedup candidates referencing these entities
	_, delErr = tx.ExecContext(ctx,
		"DELETE FROM kg_dedup_candidates WHERE (entity_a_id = ANY($1) OR entity_b_id = ANY($1)) AND agent_id = $2"+scopeSuffix,
		args...,
	)
	if delErr != nil {
		return 0, delErr
	}

	res, err := tx.ExecContext(ctx,
		"DELETE FROM kg_entities WHERE id = ANY($1) AND agent_id = $2"+scopeSuffix,
		args...,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), tx.Commit()
}

func (s *PGKnowledgeGraphStore) ListEntities(ctx context.Context, agentID, userID string, opts store.EntityListOptions) ([]store.Entity, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("kg list entities: %w", err)
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	// Build dynamic WHERE clause: always filter by agent_id, optionally by user_id and entity_type.
	// Default to current facts only (valid_until IS NULL) — expired entities excluded.
	where := "agent_id = $1 AND valid_until IS NULL"
	args := []any{aid}
	idx := 2
	userWhere, userArgs := kgUserWhere(ctx, userID, idx)
	if userWhere != "" {
		where += userWhere
		args = append(args, userArgs...)
		idx += len(userArgs)
	}
	if opts.ScopeID != "" {
		where += fmt.Sprintf(" AND user_id = $%d", idx)
		args = append(args, opts.ScopeID)
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
		idx += len(tcArgs)
	}
	args = append(args, limit, opts.Offset)
	query := fmt.Sprintf(`
		SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		       properties, source_id, confidence, created_at, updated_at, event_time
		FROM kg_entities WHERE %s
		ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, where, idx, idx+1)

	var rows []entityRow
	if err = pkgSqlxDB.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, err
	}
	entities := make([]store.Entity, len(rows))
	for i := range rows {
		entities[i] = rows[i].toEntity()
	}
	return entities, nil
}

func (s *PGKnowledgeGraphStore) SearchEntities(ctx context.Context, agentID, userID, query string, limit int) ([]store.Entity, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("kg search entities: %w", err)
	}
	if limit <= 0 {
		limit = 20
	}

	// FTS search using tsvector
	ftsResults, err := s.ftsSearchEntities(ctx, aid, userID, query, limit*2)
	if err != nil {
		return nil, err
	}

	// Vector search if provider available
	var vecResults []scoredEntity
	if s.embProvider != nil {
		embeddings, embErr := s.embProvider.Embed(ctx, []string{query})
		if embErr == nil && len(embeddings) > 0 {
			vecResults, err = s.vectorSearchEntities(ctx, embeddings[0], aid, userID, limit*2)
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
		return dedupByName(entities), nil
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

func (s *PGKnowledgeGraphStore) ftsSearchEntities(ctx context.Context, agentID uuid.UUID, userID, query string, limit int) ([]scoredEntity, error) {
	// Try AND search first (all tokens must match)
	andResults, err := s.ftsSearch(ctx, agentID, userID, query, "AND", limit)
	if err != nil {
		return nil, err
	}
	if len(andResults) >= limit {
		return andResults, nil
	}

	// Supplement with OR search to find near-duplicates the AND missed.
	// This enables dedupByName to merge stale entities with their updated versions.
	orResults, orErr := s.ftsSearch(ctx, agentID, userID, query, "OR", limit)
	if orErr != nil || len(orResults) == 0 {
		if len(andResults) > 0 {
			return andResults, nil
		}
		return orResults, orErr
	}

	// Merge: AND results first (higher relevance), then OR results not already present
	seen := make(map[string]bool, len(andResults))
	for _, r := range andResults {
		seen[r.Entity.ID] = true
	}
	merged := andResults
	for _, r := range orResults {
		if !seen[r.Entity.ID] {
			merged = append(merged, r)
			seen[r.Entity.ID] = true
		}
	}
	return merged, nil
}

// ftsSearch runs a full-text search with the given mode ("AND" or "OR").
// The search tsvector includes event_time date tokens so date-based queries match.
func (s *PGKnowledgeGraphStore) ftsSearch(ctx context.Context, agentID uuid.UUID, userID, query, mode string, limit int) ([]scoredEntity, error) {
	// Build the tsquery: AND uses plainto_tsquery, OR replaces '&' with '|'
	tsqueryExpr := fmt.Sprintf("plainto_tsquery('simple', $2)")
	if mode == "OR" {
		tsqueryExpr = fmt.Sprintf("to_tsquery('simple', replace(plainto_tsquery('simple', $2)::text, '&', '|'))")
	}

	// Include event_time date tokens in the searchable tsvector
	// so queries like "28 april" match entities with event_time on that date.
	combinedTsv := "(tsv || to_tsvector('simple', COALESCE(to_char(event_time, 'DD Month YYYY'), '')) || to_tsvector('simple', COALESCE(properties::text, '')))"
	where := fmt.Sprintf("agent_id = $1 AND valid_until IS NULL AND %s @@ %s", combinedTsv, tsqueryExpr)
	args := []any{agentID, query}
	idx := 3
	userWhere, userArgs := kgUserWhere(ctx, userID, idx)
	if userWhere != "" {
		where += userWhere
		args = append(args, userArgs...)
		idx += len(userArgs)
	}
	tc, tcArgs, _, err := scopeClause(ctx, idx)
	if err != nil {
		return nil, err
	}
	if tc != "" {
		where += tc
		args = append(args, tcArgs...)
		idx += len(tcArgs)
	}
	args = append(args, limit)
	q := fmt.Sprintf(`
		SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		       properties, source_id, confidence, created_at, updated_at, event_time,
		       ts_rank(%s, %s) * %f AS score
		FROM kg_entities
		WHERE %s
		ORDER BY score DESC, updated_at DESC LIMIT $%d`, combinedTsv, tsqueryExpr, 0.5, where, idx)

	var sRows []scoredEntityRow
	if err = pkgSqlxDB.SelectContext(ctx, &sRows, q, args...); err != nil {
		return nil, err
	}
	results := make([]scoredEntity, len(sRows))
	for i := range sRows {
		results[i] = scoredEntity{Entity: sRows[i].toEntity(), Score: sRows[i].Score}
	}
	return results, nil
}

func (s *PGKnowledgeGraphStore) vectorSearchEntities(ctx context.Context, embedding []float32, agentID uuid.UUID, userID string, limit int) ([]scoredEntity, error) {
	vecStr := vectorToString(embedding)

	where := "agent_id = $1 AND valid_until IS NULL AND embedding IS NOT NULL"
	args := []any{agentID}
	idx := 2
	userWhere, userArgs := kgUserWhere(ctx, userID, idx)
	if userWhere != "" {
		where += userWhere
		args = append(args, userArgs...)
		idx += len(userArgs)
	}
	tc, tcArgs, _, err := scopeClause(ctx, idx)
	if err != nil {
		return nil, err
	}
	if tc != "" {
		where += tc
		args = append(args, tcArgs...)
		idx += len(tcArgs)
	}
	args = append(args, vecStr, limit)
	q := fmt.Sprintf(`
		SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		       properties, source_id, confidence, created_at, updated_at, event_time,
		       1 - (embedding <=> $%d::vector) AS score
		FROM kg_entities
		WHERE %s
		ORDER BY embedding <=> $%d::vector, updated_at DESC LIMIT $%d`, idx, where, idx, idx+1)

	var sRows []scoredEntityRow
	if err = pkgSqlxDB.SelectContext(ctx, &sRows, q, args...); err != nil {
		return nil, err
	}
	results := make([]scoredEntity, len(sRows))
	for i := range sRows {
		results[i] = scoredEntity{Entity: sRows[i].toEntity(), Score: sRows[i].Score}
	}
	return results, nil
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

// dedupByName removes near-duplicate entities by normalizing names.
// When multiple entities share the same normalized name, keeps the most recently
// updated one and merges properties from older duplicates.
func dedupByName(entities []store.Entity) []store.Entity {
	if len(entities) <= 1 {
		return entities
	}

	type entry struct {
		entity store.Entity
		keep   bool
	}

	entries := make([]entry, len(entities))
	for i, e := range entities {
		entries[i] = entry{entity: e, keep: true}
	}

	for i := range entries {
		if !entries[i].keep {
			continue
		}
		ni := normalizeEntityName(entries[i].entity.Name)
		for j := i + 1; j < len(entries); j++ {
			if !entries[j].keep {
				continue
			}
			nj := normalizeEntityName(entries[j].entity.Name)
			if ni == nj {
				// Keep the newer one (entities come sorted by updated_at DESC)
				if entries[j].entity.UpdatedAt > entries[i].entity.UpdatedAt {
					mergeProps(&entries[j].entity, entries[i].entity.Properties)
					entries[i].keep = false
				} else {
					mergeProps(&entries[i].entity, entries[j].entity.Properties)
					entries[j].keep = false
				}
			}
		}
	}

	result := make([]store.Entity, 0, len(entities))
	for _, e := range entries {
		if e.keep {
			result = append(result, e.entity)
		}
	}
	return result
}

// normalizeEntityName strips common prefixes and lowercases for comparison.
func normalizeEntityName(name string) string {
	s := strings.ToLower(name)
	// Strip common prefixes that vary between extractions
	s = strings.TrimPrefix(s, "migrasi ")
	s = strings.TrimPrefix(s, "status ")
	s = strings.TrimPrefix(s, "task: ")
	// Trim spaces
	s = strings.TrimSpace(s)
	return s
}

// mergeProps merges source properties into target, only adding keys not already present.
func mergeProps(target *store.Entity, source map[string]string) {
	if source == nil {
		return
	}
	if target.Properties == nil {
		target.Properties = make(map[string]string, len(source))
	}
	for k, v := range source {
		if _, exists := target.Properties[k]; !exists && v != "" {
			target.Properties[k] = v
		}
	}
}

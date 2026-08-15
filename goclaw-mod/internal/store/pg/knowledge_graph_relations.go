package pg

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *PGKnowledgeGraphStore) UpsertRelation(ctx context.Context, relation *store.Relation) error {
	aid, err := parseUUID(relation.AgentID)
	if err != nil {
		return fmt.Errorf("kg upsert relation: agent: %w", err)
	}
	src, err := parseUUID(relation.SourceEntityID)
	if err != nil {
		return fmt.Errorf("kg upsert relation: source: %w", err)
	}
	tgt, err := parseUUID(relation.TargetEntityID)
	if err != nil {
		return fmt.Errorf("kg upsert relation: target: %w", err)
	}
	props, err := json.Marshal(relation.Properties)
	if err != nil {
		props = []byte("{}")
	}
	id := uuid.Must(uuid.NewV7())
	now := time.Now()
	tid := tenantIDForInsert(ctx)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO kg_relations
			(id, agent_id, user_id, source_entity_id, relation_type, target_entity_id, confidence, properties, tenant_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (agent_id, user_id, source_entity_id, relation_type, target_entity_id) DO UPDATE SET
			confidence  = EXCLUDED.confidence,
			properties  = EXCLUDED.properties,
			tenant_id   = EXCLUDED.tenant_id`,
		id, aid, relation.UserID, src, relation.RelationType, tgt, relation.Confidence, props, tid, now,
	)
	return err
}

func (s *PGKnowledgeGraphStore) DeleteRelation(ctx context.Context, agentID, userID, relationID string) error {
	aid, err := parseUUID(agentID)
	if err != nil {
		return fmt.Errorf("kg delete relation: agent: %w", err)
	}
	rid, err := parseUUID(relationID)
	if err != nil {
		return fmt.Errorf("kg delete relation: id: %w", err)
	}
	if store.IsSharedKG(ctx) {
		tc, tcArgs, _, err := scopeClause(ctx, 3)
		if err != nil {
			return err
		}
		_, err = s.db.ExecContext(ctx,
			`DELETE FROM kg_relations WHERE id = $1 AND agent_id = $2`+tc,
			append([]any{rid, aid}, tcArgs...)...,
		)
		return err
	}
	tc, tcArgs, _, err := scopeClause(ctx, 4)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`DELETE FROM kg_relations WHERE id = $1 AND agent_id = $2 AND user_id = $3`+tc,
		append([]any{rid, aid, userID}, tcArgs...)...,
	)
	return err
}

func (s *PGKnowledgeGraphStore) ListRelations(ctx context.Context, agentID, userID, entityID string) ([]store.Relation, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("kg list relations: agent: %w", err)
	}
	eid, err := parseUUID(entityID)
	if err != nil {
		return nil, fmt.Errorf("kg list relations: entity: %w", err)
	}

	var q string
	var args []any
	if store.IsSharedKG(ctx) {
		tc, tcArgs, _, err := scopeClause(ctx, 3)
		if err != nil {
			return nil, err
		}
		q = `SELECT id, agent_id, user_id, source_entity_id, relation_type, target_entity_id,
		       confidence, properties, created_at
		FROM kg_relations
		WHERE agent_id = $1 AND valid_until IS NULL
		  AND (source_entity_id = $2 OR target_entity_id = $2)` + tc + `
		ORDER BY created_at DESC`
		args = append([]any{aid, eid}, tcArgs...)
	} else {
		tc, tcArgs, _, err := scopeClause(ctx, 4)
		if err != nil {
			return nil, err
		}
		q = `SELECT id, agent_id, user_id, source_entity_id, relation_type, target_entity_id,
		       confidence, properties, created_at
		FROM kg_relations
		WHERE agent_id = $1 AND user_id = $2 AND valid_until IS NULL
		  AND (source_entity_id = $3 OR target_entity_id = $3)` + tc + `
		ORDER BY created_at DESC`
		args = append([]any{aid, userID, eid}, tcArgs...)
	}

	var rRows []relationRow
	if err := pkgSqlxDB.SelectContext(ctx, &rRows, q, args...); err != nil {
		return nil, err
	}
	result := make([]store.Relation, len(rRows))
	for i := range rRows {
		result[i] = rRows[i].toRelation()
	}
	return result, nil
}

func (s *PGKnowledgeGraphStore) ListAllRelations(ctx context.Context, agentID, userID string, limit int) ([]store.Relation, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("kg list all relations: %w", err)
	}
	if limit <= 0 {
		limit = 200
	}
	where := "agent_id = $1 AND valid_until IS NULL"
	args := []any{aid}
	idx := 2
	if !store.IsSharedKG(ctx) && userID != "" {
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
	args = append(args, limit)
	q := fmt.Sprintf(`
		SELECT id, agent_id, user_id, source_entity_id, relation_type, target_entity_id,
		       confidence, properties, created_at
		FROM kg_relations WHERE %s
		ORDER BY created_at DESC LIMIT $%d`, where, idx)
	var rRows []relationRow
	if err = pkgSqlxDB.SelectContext(ctx, &rRows, q, args...); err != nil {
		return nil, err
	}
	result := make([]store.Relation, len(rRows))
	for i := range rRows {
		result[i] = rRows[i].toRelation()
	}
	return result, nil
}

func (s *PGKnowledgeGraphStore) IngestExtraction(ctx context.Context, agentID, userID string, entities []store.Entity, relations []store.Relation) ([]string, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("kg ingest extraction: agent: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now()
	tid := tenantIDForInsert(ctx)

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
			id, aid, userID, e.ExternalID, e.Name, e.EntityType,
			e.Description, props, e.SourceID, e.Confidence, tid, now,
		).Scan(&actualID); err != nil {
			return nil, err
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
				(id, agent_id, user_id, source_entity_id, relation_type, target_entity_id, confidence, properties, tenant_id, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (agent_id, user_id, source_entity_id, relation_type, target_entity_id) DO UPDATE SET
				confidence  = EXCLUDED.confidence,
				properties  = EXCLUDED.properties,
				tenant_id   = EXCLUDED.tenant_id`,
			id, aid, userID, src, r.RelationType, tgt, r.Confidence, props, tid, now,
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Collect upserted entity IDs for downstream processing (e.g. dedup)
	entityIDs := make([]string, 0, len(extIDToUUID))
	for _, uid := range extIDToUUID {
		entityIDs = append(entityIDs, uid.String())
	}
	return entityIDs, nil
}

func (s *PGKnowledgeGraphStore) PruneByConfidence(ctx context.Context, agentID, userID string, minConfidence float64) (int, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return 0, fmt.Errorf("kg prune: %w", err)
	}
	var res sql.Result
	if store.IsSharedKG(ctx) {
		tc, tcArgs, _, tcErr := scopeClause(ctx, 3)
		if tcErr != nil {
			return 0, tcErr
		}
		res, err = s.db.ExecContext(ctx,
			`DELETE FROM kg_entities WHERE agent_id = $1 AND confidence < $2`+tc,
			append([]any{aid, minConfidence}, tcArgs...)...,
		)
	} else {
		tc, tcArgs, _, tcErr := scopeClause(ctx, 4)
		if tcErr != nil {
			return 0, tcErr
		}
		res, err = s.db.ExecContext(ctx,
			`DELETE FROM kg_entities WHERE agent_id = $1 AND user_id = $2 AND confidence < $3`+tc,
			append([]any{aid, userID, minConfidence}, tcArgs...)...,
		)
	}
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *PGKnowledgeGraphStore) Stats(ctx context.Context, agentID, userID string) (*store.GraphStats, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("kg stats: %w", err)
	}
	stats := &store.GraphStats{EntityTypes: make(map[string]int)}

	userFilter := ""
	args := []any{aid}
	idx := 2
	if userID != "" {
		userFilter = fmt.Sprintf(" AND user_id = $%d", idx)
		args = append(args, userID)
		idx++
	}
	tc, tcArgs, _, err := scopeClause(ctx, idx)
	if err != nil {
		return nil, err
	}
	tenantFilter := tc
	args = append(args, tcArgs...)

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kg_entities WHERE agent_id = $1 AND valid_until IS NULL`+userFilter+tenantFilter, args...,
	).Scan(&stats.EntityCount); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM kg_relations WHERE agent_id = $1 AND valid_until IS NULL`+userFilter+tenantFilter, args...,
	).Scan(&stats.RelationCount); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT entity_type, COUNT(*) FROM kg_entities WHERE agent_id = $1 AND valid_until IS NULL`+userFilter+tenantFilter+` GROUP BY entity_type`, args...,
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

	// Fetch distinct user IDs (only when not filtering by specific user)
	if userID == "" {
		uidRows, uidErr := s.db.QueryContext(ctx,
			`SELECT DISTINCT user_id FROM kg_entities WHERE agent_id = $1`+tenantFilter+` AND user_id != '' ORDER BY user_id`,
			append([]any{aid}, tcArgs...)...,
		)
		if uidErr == nil {
			defer uidRows.Close()
			for uidRows.Next() {
				var uid string
				if uidRows.Scan(&uid) == nil && uid != "" {
					stats.UserIDs = append(stats.UserIDs, uid)
				}
			}
		}
	}

	return stats, nil
}

func (s *PGKnowledgeGraphStore) Close() error { return nil }

package pg

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	kg "github.com/nextlevelbuilder/goclaw/internal/knowledgegraph"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	dedupAutoMergeThreshold = 0.98
	dedupCandidateThreshold = 0.90
	dedupNameMatchThreshold = 0.85
)

// DedupAfterExtraction checks newly upserted entities for duplicates using
// embedding similarity (HNSW KNN) and name similarity (Jaro-Winkler).
// Auto-merges near-certain duplicates (>0.98 + name match), flags possible
// duplicates (>0.90) as candidates for manual review.
func (s *PGKnowledgeGraphStore) DedupAfterExtraction(ctx context.Context, agentID, userID string, newEntityIDs []string) (int, int, error) {
	if len(newEntityIDs) == 0 {
		return 0, 0, nil
	}

	aid, err := parseUUID(agentID)
	if err != nil {
		return 0, 0, fmt.Errorf("kg dedup after extraction: %w", err)
	}
	shared := store.IsSharedKG(ctx)
	var merged, flagged int

	for _, eid := range newEntityIDs {
		entityID, parseErr := uuid.Parse(eid)
		if parseErr != nil {
			slog.Warn("kg.dedup: invalid entity ID", "id", eid, "error", parseErr)
			continue
		}

		// Fetch entity details + embedding with tenant scope
		var name, entityType string
		var embeddingStr *string
		var confidence float64
		tc, tcArgs, _, err := scopeClause(ctx, 3)
		if err != nil {
			continue
		}
		row := s.db.QueryRowContext(ctx,
			`SELECT name, entity_type, confidence, embedding::text
			 FROM kg_entities WHERE id = $1 AND agent_id = $2`+tc,
			append([]any{entityID, aid}, tcArgs...)...)
		if err := row.Scan(&name, &entityType, &confidence, &embeddingStr); err != nil {
			continue // entity may have been deleted/merged already
		}
		if embeddingStr == nil {
			continue // no embedding → can't compute similarity
		}

		// KNN: find top-3 nearest existing entities of same type (exclude self)
		neighbors, err := s.knnNeighbors(ctx, aid, userID, entityID, entityType, *embeddingStr, shared, 3)
		if err != nil {
			slog.Warn("kg.dedup: knn query failed", "entity_id", eid, "error", err)
			continue
		}

		for _, n := range neighbors {
			nameSim := kg.JaroWinkler(name, n.name)

			if n.similarity >= dedupAutoMergeThreshold && nameSim >= dedupNameMatchThreshold {
				// Auto-merge: keep the one with higher confidence
				targetID, sourceID := eid, n.id
				if n.confidence > confidence {
					targetID, sourceID = n.id, eid
				}
				if err := s.MergeEntities(ctx, agentID, userID, targetID, sourceID); err != nil {
					slog.Warn("kg.dedup: auto-merge failed", "target", targetID, "source", sourceID, "error", err)
					continue
				}
				merged++
				break // entity merged, stop checking neighbors
			} else if n.similarity >= dedupCandidateThreshold {
				// Flag as candidate for manual review
				if err := s.insertDedupCandidate(ctx, aid, userID, eid, n.id, n.similarity); err != nil {
					slog.Warn("kg.dedup: flag candidate failed", "error", err)
				} else {
					flagged++
				}
			}
		}
	}

	return merged, flagged, nil
}

type knnNeighbor struct {
	id         string
	name       string
	confidence float64
	similarity float64
}

// knnNeighbors finds the top-K nearest entities of the same type using HNSW index.
// embeddingStr is the PG vector text format (e.g. "[0.1,0.2,...]").
func (s *PGKnowledgeGraphStore) knnNeighbors(ctx context.Context, agentID uuid.UUID, userID string, excludeID uuid.UUID, entityType, embeddingStr string, shared bool, limit int) ([]knnNeighbor, error) {
	where := "agent_id = $1 AND entity_type = $2 AND id != $3 AND embedding IS NOT NULL"
	args := []any{agentID, entityType, excludeID}
	idx := 4
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
	args = append(args, embeddingStr, limit)
	q := fmt.Sprintf(`
		SELECT id, name, confidence,
		       1 - (embedding <=> $%d::vector) AS similarity
		FROM kg_entities
		WHERE %s
		ORDER BY embedding <=> $%d::vector
		LIMIT $%d`, idx, where, idx, idx+1)

	var nRows []knnNeighborRow
	if err = pkgSqlxDB.SelectContext(ctx, &nRows, q, args...); err != nil {
		return nil, err
	}
	results := make([]knnNeighbor, len(nRows))
	for i, r := range nRows {
		results[i] = knnNeighbor{id: r.ID, name: r.Name, confidence: r.Confidence, similarity: r.Similarity}
	}
	return results, nil
}

func (s *PGKnowledgeGraphStore) insertDedupCandidate(ctx context.Context, agentID uuid.UUID, userID, entityAID, entityBID string, similarity float64) error {
	// Ensure consistent ordering (smaller UUID first) to avoid duplicates
	if entityAID > entityBID {
		entityAID, entityBID = entityBID, entityAID
	}
	aID, err := parseUUID(entityAID)
	if err != nil {
		return fmt.Errorf("insert dedup candidate: entity_a_id: %w", err)
	}
	bID, err := parseUUID(entityBID)
	if err != nil {
		return fmt.Errorf("insert dedup candidate: entity_b_id: %w", err)
	}
	tid := tenantIDForInsert(ctx)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO kg_dedup_candidates (id, tenant_id, agent_id, user_id, entity_a_id, entity_b_id, similarity, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (entity_a_id, entity_b_id) DO NOTHING`,
		uuid.Must(uuid.NewV7()), tid, agentID, userID, aID, bID, similarity, time.Now(),
	)
	return err
}

// ScanDuplicates performs a bulk scan of ALL entities with embeddings using
// a self-join to find duplicate candidates above the given threshold.
// Inserts results into kg_dedup_candidates. Returns number of candidates found.
func (s *PGKnowledgeGraphStore) ScanDuplicates(ctx context.Context, agentID, userID string, threshold float64, limit int) (int, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return 0, fmt.Errorf("kg scan duplicates: %w", err)
	}
	if threshold <= 0 {
		threshold = dedupCandidateThreshold
	}
	if limit <= 0 {
		limit = 100
	}

	shared := store.IsSharedKG(ctx)

	where := "a.agent_id = $1"
	args := []any{aid}
	idx := 2

	if !shared && userID != "" {
		where += fmt.Sprintf(" AND a.user_id = $%d", idx)
		args = append(args, userID)
		idx++
	}
	tc, tcArgs, _, err := scopeClauseAlias(ctx, idx, "a")
	if err != nil {
		return 0, err
	}
	if tc != "" {
		where += tc
		args = append(args, tcArgs...)
		idx += len(tcArgs)
	}
	args = append(args, threshold, limit)

	q := fmt.Sprintf(`
		SELECT a.id, b.id, 1 - (a.embedding <=> b.embedding) AS similarity
		FROM kg_entities a
		JOIN kg_entities b ON b.agent_id = a.agent_id
		  AND b.tenant_id = a.tenant_id
		  AND b.entity_type = a.entity_type
		  AND b.id > a.id
		  AND b.embedding IS NOT NULL
		WHERE %s
		  AND a.embedding IS NOT NULL
		  AND 1 - (a.embedding <=> b.embedding) > $%d
		ORDER BY similarity DESC
		LIMIT $%d`, where, idx, idx+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, fmt.Errorf("kg.scan_duplicates: query failed: %w", err)
	}
	defer rows.Close()

	tid := tenantIDForInsert(ctx)
	found := 0
	for rows.Next() {
		var aID, bID string
		var sim float64
		if err := rows.Scan(&aID, &bID, &sim); err != nil {
			continue
		}
		// Ensure consistent ordering
		if aID > bID {
			aID, bID = bID, aID
		}
		aUUID, err := parseUUID(aID)
		if err != nil {
			slog.Warn("kg.scan_duplicates: invalid entity_a UUID from DB row", "id", aID, "error", err)
			continue
		}
		bUUID, err := parseUUID(bID)
		if err != nil {
			slog.Warn("kg.scan_duplicates: invalid entity_b UUID from DB row", "id", bID, "error", err)
			continue
		}
		if _, err := s.db.ExecContext(ctx, `
			INSERT INTO kg_dedup_candidates (id, tenant_id, agent_id, user_id, entity_a_id, entity_b_id, similarity, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (entity_a_id, entity_b_id) DO NOTHING`,
			uuid.Must(uuid.NewV7()), tid, aid, userID, aUUID, bUUID, sim, time.Now(),
		); err != nil {
			slog.Warn("kg.scan_duplicates: insert candidate failed", "error", err)
			continue
		}
		found++
	}

	return found, rows.Err()
}

// MergeEntities merges sourceID into targetID: re-points all relations from
// source to target, deletes the source entity. Uses advisory lock to prevent
// concurrent merges on the same agent.
func (s *PGKnowledgeGraphStore) MergeEntities(ctx context.Context, agentID, userID, targetID, sourceID string) error {
	aid, err := parseUUID(agentID)
	if err != nil {
		return fmt.Errorf("kg merge entities: agent: %w", err)
	}
	tid, err := parseUUID(targetID)
	if err != nil {
		return fmt.Errorf("kg merge entities: target: %w", err)
	}
	sid, err := parseUUID(sourceID)
	if err != nil {
		return fmt.Errorf("kg merge entities: source: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	// Advisory lock per agent to prevent concurrent merges
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, agentID); err != nil {
		return fmt.Errorf("kg.merge: advisory lock failed: %w", err)
	}

	// Verify both entities exist and belong to the same agent + tenant scope.
	// When userID is empty, skip user_id filter (admin/shared view).
	shared := store.IsSharedKG(ctx) || userID == ""
	for _, eid := range []uuid.UUID{tid, sid} {
		var exists bool
		var q string
		var args []any
		if shared {
			tc, tcArgs, _, err := scopeClause(ctx, 3)
			if err != nil {
				return err
			}
			q = `SELECT EXISTS(SELECT 1 FROM kg_entities WHERE id = $1 AND agent_id = $2` + tc + `)`
			args = append([]any{eid, aid}, tcArgs...)
		} else {
			tc, tcArgs, _, err := scopeClause(ctx, 4)
			if err != nil {
				return err
			}
			q = `SELECT EXISTS(SELECT 1 FROM kg_entities WHERE id = $1 AND agent_id = $2 AND user_id = $3` + tc + `)`
			args = append([]any{eid, aid, userID}, tcArgs...)
		}
		if err := tx.QueryRowContext(ctx, q, args...).Scan(&exists); err != nil {
			return fmt.Errorf("kg.merge: entity check failed: %w", err)
		}
		if !exists {
			return fmt.Errorf("kg.merge: entity %s not found or access denied", eid)
		}
	}

	// Re-point relations from source to target.
	// First delete relations that would become duplicates after re-pointing,
	// then update the remaining ones.
	tc, tcArgs, _, err := scopeClause(ctx, 4)
	if err != nil {
		return err
	}
	for _, cols := range [][2]string{
		{"source_entity_id", "target_entity_id"},
		{"target_entity_id", "source_entity_id"},
	} {
		col, otherCol := cols[0], cols[1]
		// Delete would-be-duplicate relations (same type, same endpoints after re-point)
		delQ := fmt.Sprintf(`
			DELETE FROM kg_relations r1
			WHERE r1.%s = $1 AND r1.agent_id = $2
			AND EXISTS (
				SELECT 1 FROM kg_relations r2
				WHERE r2.%s = $3
				AND r2.agent_id = r1.agent_id
				AND r2.user_id = r1.user_id
				AND r2.relation_type = r1.relation_type
				AND r2.%s = r1.%s
			)`, col, col, otherCol, otherCol)
		delArgs := append([]any{sid, aid, tid}, tcArgs...)
		if _, err := tx.ExecContext(ctx, delQ+tc, delArgs...); err != nil {
			return fmt.Errorf("kg.merge: dedup relations %s failed: %w", col, err)
		}
		// Update remaining relations
		updQ := fmt.Sprintf(`UPDATE kg_relations SET %s = $1 WHERE %s = $2 AND agent_id = $3`, col, col)
		updArgs := append([]any{tid, sid, aid}, tcArgs...)
		if _, err := tx.ExecContext(ctx, updQ+tc, updArgs...); err != nil {
			return fmt.Errorf("kg.merge: re-point %s failed: %w", col, err)
		}
	}

	// Delete the source entity (CASCADE removes any remaining orphan relations)
	if _, err := tx.ExecContext(ctx, `DELETE FROM kg_entities WHERE id = $1`, sid); err != nil {
		return fmt.Errorf("kg.merge: delete source failed: %w", err)
	}

	// Mark any dedup candidates referencing the source as merged
	if _, err := tx.ExecContext(ctx, `
		UPDATE kg_dedup_candidates SET status = 'merged'
		WHERE (entity_a_id = $1 OR entity_b_id = $1) AND status = 'pending'`, sid); err != nil {
		slog.Warn("kg.merge: update candidates failed", "error", err)
	}

	return tx.Commit()
}

// ListDedupCandidates returns pending dedup candidates for review.
func (s *PGKnowledgeGraphStore) ListDedupCandidates(ctx context.Context, agentID, userID string, limit int) ([]store.DedupCandidate, error) {
	aid, err := parseUUID(agentID)
	if err != nil {
		return nil, fmt.Errorf("kg list dedup candidates: %w", err)
	}
	if limit <= 0 {
		limit = 50
	}

	where := "c.agent_id = $1 AND c.status = 'pending'"
	args := []any{aid}
	idx := 2
	if !store.IsSharedKG(ctx) && userID != "" {
		where += fmt.Sprintf(" AND c.user_id = $%d", idx)
		args = append(args, userID)
		idx++
	}
	tc, tcArgs, _, err := scopeClauseAlias(ctx, idx, "c")
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
		SELECT c.id, c.similarity, c.status, c.created_at,
		       a.id AS a_id, a.agent_id AS a_agent_id, a.user_id AS a_user_id,
		       a.external_id AS a_external_id, a.name AS a_entity_name, a.entity_type AS a_entity_type,
		       a.description AS a_description, a.properties AS a_properties,
		       a.source_id AS a_source_id, a.confidence AS a_confidence,
		       a.created_at AS a_created_at, a.updated_at AS a_updated_at,
		       b.id AS b_id, b.agent_id AS b_agent_id, b.user_id AS b_user_id,
		       b.external_id AS b_external_id, b.name AS b_entity_name, b.entity_type AS b_entity_type,
		       b.description AS b_description, b.properties AS b_properties,
		       b.source_id AS b_source_id, b.confidence AS b_confidence,
		       b.created_at AS b_created_at, b.updated_at AS b_updated_at
		FROM kg_dedup_candidates c
		JOIN kg_entities a ON c.entity_a_id = a.id
		JOIN kg_entities b ON c.entity_b_id = b.id
		WHERE %s
		ORDER BY c.similarity DESC, c.created_at DESC
		LIMIT $%d`, where, idx)

	var dRows []dedupCandidateRow
	if err = pkgSqlxDB.SelectContext(ctx, &dRows, q, args...); err != nil {
		return nil, err
	}
	results := make([]store.DedupCandidate, len(dRows))
	for i := range dRows {
		results[i] = dRows[i].toDedupCandidate()
	}
	return results, nil
}

// DismissCandidate marks a dedup candidate as dismissed.
// Scoped by agent_id + tenant to prevent cross-agent/cross-tenant dismissal.
func (s *PGKnowledgeGraphStore) DismissCandidate(ctx context.Context, agentID, candidateID string) error {
	aid, err := parseUUID(agentID)
	if err != nil {
		return fmt.Errorf("kg dismiss candidate: agent: %w", err)
	}
	cid, err := parseUUID(candidateID)
	if err != nil {
		return fmt.Errorf("kg dismiss candidate: id: %w", err)
	}
	tc, tcArgs, _, err := scopeClause(ctx, 3)
	if err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE kg_dedup_candidates SET status = 'dismissed' WHERE id = $1 AND agent_id = $2 AND status = 'pending'`+tc,
		append([]any{cid, aid}, tcArgs...)...,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

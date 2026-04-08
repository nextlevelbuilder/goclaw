//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	kg "github.com/nextlevelbuilder/goclaw/internal/knowledgegraph"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const (
	kgDedupAutoThreshold = 0.98
	kgDedupFlagThreshold = 0.90
	kgDedupNameThreshold = 0.85
	kgDedupEntityCap     = 200
)

// IngestExtraction upserts entities and relations from an LLM extraction in a single transaction.
// Returns DB UUIDs of all upserted entities for downstream dedup processing.
func (s *SQLiteKnowledgeGraphStore) IngestExtraction(ctx context.Context, agentID, userID string, entities []store.Entity, relations []store.Relation) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().UTC().Format(time.RFC3339Nano)
	tid := tenantIDForInsert(ctx).String()

	// Upsert entities; build external_id → DB ID lookup for resolving relation endpoints.
	extIDtoDBID := make(map[string]string, len(entities))
	for i := range entities {
		e := &entities[i]
		e.AgentID = agentID
		e.UserID = userID

		props, _ := json.Marshal(e.Properties)
		if props == nil {
			props = []byte("{}")
		}
		newID := uuid.Must(uuid.NewV7()).String()

		var actualID string
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO kg_entities
				(id, agent_id, user_id, external_id, name, entity_type, description,
				 properties, source_id, confidence, tenant_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(agent_id, user_id, external_id) DO UPDATE SET
				name        = excluded.name,
				entity_type = excluded.entity_type,
				description = excluded.description,
				properties  = excluded.properties,
				source_id   = excluded.source_id,
				confidence  = excluded.confidence,
				tenant_id   = excluded.tenant_id,
				updated_at  = excluded.updated_at
			RETURNING id`,
			newID, agentID, userID, e.ExternalID, e.Name, e.EntityType,
			e.Description, string(props), e.SourceID, e.Confidence, tid, now, now,
		).Scan(&actualID); err != nil {
			return nil, fmt.Errorf("ingest entity %q: %w", e.ExternalID, err)
		}
		extIDtoDBID[e.ExternalID] = actualID
		e.ID = actualID
	}

	for i := range relations {
		r := &relations[i]
		srcID, ok1 := extIDtoDBID[r.SourceEntityID]
		tgtID, ok2 := extIDtoDBID[r.TargetEntityID]
		if !ok1 || !ok2 {
			continue // skip relations referencing unknown entities
		}
		r.AgentID = agentID
		r.UserID = userID
		origSrc, origTgt := r.SourceEntityID, r.TargetEntityID
		r.SourceEntityID = srcID
		r.TargetEntityID = tgtID
		if err := upsertRelationTx(ctx, tx, agentID, userID, r, tid, now); err != nil {
			return nil, fmt.Errorf("ingest relation %s->%s: %w", origSrc, origTgt, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	entityIDs := make([]string, 0, len(extIDtoDBID))
	for _, id := range extIDtoDBID {
		entityIDs = append(entityIDs, id)
	}
	return entityIDs, nil
}

// PruneByConfidence deletes entities with confidence below minConfidence.
func (s *SQLiteKnowledgeGraphStore) PruneByConfidence(ctx context.Context, agentID, userID string, minConfidence float64) (int, error) {
	userClause, userArgs := kgUserClauseFor(ctx, userID)
	sc, scArgs, _ := scopeClause(ctx)
	args := append([]any{agentID, minConfidence}, userArgs...)
	args = append(args, scArgs...)

	res, err := s.db.ExecContext(ctx,
		`DELETE FROM kg_entities WHERE agent_id = ? AND confidence < ?`+userClause+sc,
		args...,
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// DedupAfterExtraction checks newly upserted entities for duplicates using
// Go-side Jaro-Winkler similarity. Auto-merges at >0.98 + name match,
// flags >0.90 as candidates. Caps existing entity pool at kgDedupEntityCap.
func (s *SQLiteKnowledgeGraphStore) DedupAfterExtraction(ctx context.Context, agentID, userID string, newEntityIDs []string) (int, int, error) {
	if len(newEntityIDs) == 0 {
		return 0, 0, nil
	}

	newEntities, err := s.fetchEntitiesByIDs(ctx, agentID, newEntityIDs)
	if err != nil {
		return 0, 0, fmt.Errorf("dedup fetch new: %w", err)
	}

	userClause, userArgs := kgUserClauseFor(ctx, userID)
	sc, scArgs, _ := scopeClause(ctx)
	poolArgs := append([]any{agentID}, userArgs...)
	poolArgs = append(poolArgs, scArgs...)
	poolArgs = append(poolArgs, kgDedupEntityCap+1)

	poolRows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		        properties, source_id, confidence, created_at, updated_at
		 FROM kg_entities
		 WHERE agent_id = ? AND valid_until IS NULL`+userClause+sc+`
		 ORDER BY updated_at DESC LIMIT ?`,
		poolArgs...,
	)
	if err != nil {
		return 0, 0, fmt.Errorf("dedup fetch pool: %w", err)
	}
	existing, scanErr := scanEntityRows(poolRows)
	poolRows.Close()
	if scanErr != nil {
		return 0, 0, scanErr
	}
	if len(existing) >= kgDedupEntityCap {
		slog.Warn("kg.dedup: entity count exceeds comparison cap", "count", len(existing), "cap", kgDedupEntityCap)
		existing = existing[:kgDedupEntityCap]
	}

	newIDSet := make(map[string]bool, len(newEntityIDs))
	for _, id := range newEntityIDs {
		newIDSet[id] = true
	}

	var merged, flagged int

	for i := range newEntities {
		newE := &newEntities[i]
		newText := newE.Name + " " + newE.Description

		for j := range existing {
			existE := &existing[j]
			if existE.ID == newE.ID {
				continue
			}
			if newIDSet[existE.ID] {
				continue // both new — avoid double-processing
			}
			existText := existE.Name + " " + existE.Description
			sim := kg.JaroWinkler(newText, existText)

			if sim >= kgDedupAutoThreshold {
				nameSim := kg.JaroWinkler(newE.Name, existE.Name)
				if nameSim >= kgDedupNameThreshold {
					targetID, sourceID := newE.ID, existE.ID
					if existE.Confidence > newE.Confidence {
						targetID, sourceID = existE.ID, newE.ID
					}
					if mergeErr := s.MergeEntities(ctx, agentID, userID, targetID, sourceID); mergeErr != nil {
						slog.Warn("kg.dedup: auto-merge failed", "target", targetID, "source", sourceID, "error", mergeErr)
						continue
					}
					merged++
					break
				}
			} else if sim >= kgDedupFlagThreshold {
				if flagErr := s.insertDedupCandidate(ctx, agentID, userID, newE.ID, existE.ID, sim); flagErr != nil {
					slog.Warn("kg.dedup: flag candidate failed", "error", flagErr)
				} else {
					flagged++
				}
			}
		}
	}

	return merged, flagged, nil
}

// ScanDuplicates performs a bulk scan of all entities using Go-side pairwise
// Jaro-Winkler. 2-pass: load distinct entity_types, then compare within each type.
// Caps per-type pool at kgDedupEntityCap. Returns number of candidates inserted.
func (s *SQLiteKnowledgeGraphStore) ScanDuplicates(ctx context.Context, agentID, userID string, threshold float64, limit int) (int, error) {
	if threshold <= 0 {
		threshold = kgDedupFlagThreshold
	}
	if limit <= 0 {
		limit = 100
	}

	userClause, userArgs := kgUserClauseFor(ctx, userID)
	sc, scArgs, _ := scopeClause(ctx)
	typeArgs := append([]any{agentID}, userArgs...)
	typeArgs = append(typeArgs, scArgs...)

	typeRows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT entity_type FROM kg_entities
		 WHERE agent_id = ? AND valid_until IS NULL`+userClause+sc,
		typeArgs...,
	)
	if err != nil {
		return 0, fmt.Errorf("kg.scan_duplicates: type query: %w", err)
	}
	var entityTypes []string
	for typeRows.Next() {
		var t string
		if typeRows.Scan(&t) == nil {
			entityTypes = append(entityTypes, t)
		}
	}
	typeRows.Close()

	found := 0
	for _, entityType := range entityTypes {
		typeEntityArgs := append([]any{agentID, entityType}, userArgs...)
		typeEntityArgs = append(typeEntityArgs, scArgs...)
		typeEntityArgs = append(typeEntityArgs, kgDedupEntityCap+1)

		eRows, err := s.db.QueryContext(ctx,
			`SELECT id, agent_id, user_id, external_id, name, entity_type, description,
			        properties, source_id, confidence, created_at, updated_at
			 FROM kg_entities
			 WHERE agent_id = ? AND entity_type = ? AND valid_until IS NULL`+userClause+sc+`
			 ORDER BY updated_at DESC LIMIT ?`,
			typeEntityArgs...,
		)
		if err != nil {
			slog.Warn("kg.scan_duplicates: entity load failed", "type", entityType, "error", err)
			continue
		}
		pool, scanErr := scanEntityRows(eRows)
		eRows.Close()
		if scanErr != nil {
			continue
		}
		if len(pool) >= kgDedupEntityCap {
			slog.Warn("kg.scan_duplicates: entity count exceeds comparison cap", "type", entityType, "count", len(pool), "cap", kgDedupEntityCap)
			pool = pool[:kgDedupEntityCap]
		}

		for i := 0; i < len(pool); i++ {
			for j := i + 1; j < len(pool); j++ {
				if found >= limit {
					return found, nil
				}
				a, b := &pool[i], &pool[j]
				sim := kg.JaroWinkler(a.Name+" "+a.Description, b.Name+" "+b.Description)
				if sim >= threshold {
					if insertErr := s.insertDedupCandidate(ctx, agentID, userID, a.ID, b.ID, sim); insertErr != nil {
						slog.Warn("kg.scan_duplicates: insert candidate failed", "error", insertErr)
						continue
					}
					found++
				}
			}
		}
	}

	return found, nil
}

// ListDedupCandidates returns pending dedup candidates joined with entity details.
func (s *SQLiteKnowledgeGraphStore) ListDedupCandidates(ctx context.Context, agentID, userID string, limit int) ([]store.DedupCandidate, error) {
	if limit <= 0 {
		limit = 50
	}

	userClause, userArgs := kgUserClauseFor(ctx, userID)
	sc, scArgs, _ := scopeClause(ctx)

	where := "c.agent_id = ? AND c.status = 'pending'"
	args := []any{agentID}
	if userClause != "" {
		where += strings.ReplaceAll(userClause, " user_id", " c.user_id")
		args = append(args, userArgs...)
	}
	if sc != "" {
		where += strings.ReplaceAll(sc, " tenant_id", " c.tenant_id")
		args = append(args, scArgs...)
	}
	args = append(args, limit)

	q := `
		SELECT c.id, c.similarity, c.status, c.created_at,
		       a.id, a.agent_id, a.user_id, a.external_id, a.name, a.entity_type,
		       a.description, a.properties, a.source_id, a.confidence, a.created_at, a.updated_at,
		       b.id, b.agent_id, b.user_id, b.external_id, b.name, b.entity_type,
		       b.description, b.properties, b.source_id, b.confidence, b.created_at, b.updated_at
		FROM kg_dedup_candidates c
		JOIN kg_entities a ON c.entity_a_id = a.id
		JOIN kg_entities b ON c.entity_b_id = b.id
		WHERE ` + where + `
		ORDER BY c.similarity DESC, c.created_at DESC LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []store.DedupCandidate
	for rows.Next() {
		var dc store.DedupCandidate
		var cCreatedAt, aCreatedAt, aUpdatedAt, bCreatedAt, bUpdatedAt any
		var aProps, bProps []byte

		if err := rows.Scan(
			&dc.ID, &dc.Similarity, &dc.Status, &cCreatedAt,
			&dc.EntityA.ID, &dc.EntityA.AgentID, &dc.EntityA.UserID, &dc.EntityA.ExternalID,
			&dc.EntityA.Name, &dc.EntityA.EntityType, &dc.EntityA.Description,
			&aProps, &dc.EntityA.SourceID, &dc.EntityA.Confidence, &aCreatedAt, &aUpdatedAt,
			&dc.EntityB.ID, &dc.EntityB.AgentID, &dc.EntityB.UserID, &dc.EntityB.ExternalID,
			&dc.EntityB.Name, &dc.EntityB.EntityType, &dc.EntityB.Description,
			&bProps, &dc.EntityB.SourceID, &dc.EntityB.Confidence, &bCreatedAt, &bUpdatedAt,
		); err != nil {
			return nil, err
		}
		dc.CreatedAt = scanUnixTimestamp(cCreatedAt)
		dc.EntityA.CreatedAt = scanUnixTimestamp(aCreatedAt)
		dc.EntityA.UpdatedAt = scanUnixTimestamp(aUpdatedAt)
		dc.EntityB.CreatedAt = scanUnixTimestamp(bCreatedAt)
		dc.EntityB.UpdatedAt = scanUnixTimestamp(bUpdatedAt)
		if p, _ := scanJSONStringMap(aProps); p != nil {
			dc.EntityA.Properties = p
		}
		if p, _ := scanJSONStringMap(bProps); p != nil {
			dc.EntityB.Properties = p
		}
		results = append(results, dc)
	}
	return results, rows.Err()
}

// MergeEntities merges sourceID into targetID: re-points relations, deletes source.
// Batches relation re-pointing in chunks of 200 IDs.
func (s *SQLiteKnowledgeGraphStore) MergeEntities(ctx context.Context, agentID, userID, targetID, sourceID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	shared := store.IsSharedKG(ctx) || userID == ""
	for _, eid := range []string{targetID, sourceID} {
		var exists bool
		var q string
		var args []any
		if shared {
			q = `SELECT EXISTS(SELECT 1 FROM kg_entities WHERE id = ? AND agent_id = ?)`
			args = []any{eid, agentID}
		} else {
			q = `SELECT EXISTS(SELECT 1 FROM kg_entities WHERE id = ? AND agent_id = ? AND user_id = ?)`
			args = []any{eid, agentID, userID}
		}
		if err := tx.QueryRowContext(ctx, q, args...).Scan(&exists); err != nil {
			return fmt.Errorf("kg.merge: entity check: %w", err)
		}
		if !exists {
			return fmt.Errorf("kg.merge: entity %s not found or access denied", eid)
		}
	}

	for _, cols := range [][2]string{
		{"source_entity_id", "target_entity_id"},
		{"target_entity_id", "source_entity_id"},
	} {
		col, otherCol := cols[0], cols[1]

		// Delete would-be-duplicate relations after re-point
		delQ := fmt.Sprintf(`
			DELETE FROM kg_relations
			WHERE %s = ? AND agent_id = ?
			AND EXISTS (
				SELECT 1 FROM kg_relations r2
				WHERE r2.%s = ?
				  AND r2.agent_id = kg_relations.agent_id
				  AND r2.user_id = kg_relations.user_id
				  AND r2.relation_type = kg_relations.relation_type
				  AND r2.%s = kg_relations.%s
			)`, col, col, otherCol, otherCol)
		if _, err := tx.ExecContext(ctx, delQ, sourceID, agentID, targetID); err != nil {
			return fmt.Errorf("kg.merge: dedup relations %s: %w", col, err)
		}

		// Collect relation IDs to re-point, then batch-update in 200-ID chunks
		relIDRows, err := tx.QueryContext(ctx,
			fmt.Sprintf(`SELECT id FROM kg_relations WHERE %s = ? AND agent_id = ?`, col),
			sourceID, agentID,
		)
		if err != nil {
			return fmt.Errorf("kg.merge: fetch relation IDs: %w", err)
		}
		var relIDs []string
		for relIDRows.Next() {
			var rid string
			if relIDRows.Scan(&rid) == nil {
				relIDs = append(relIDs, rid)
			}
		}
		relIDRows.Close()

		for i := 0; i < len(relIDs); i += 200 {
			end := i + 200
			if end > len(relIDs) {
				end = len(relIDs)
			}
			chunk := relIDs[i:end]
			ph := strings.Repeat("?,", len(chunk))
			ph = ph[:len(ph)-1]
			updArgs := []any{targetID}
			for _, rid := range chunk {
				updArgs = append(updArgs, rid)
			}
			if _, err := tx.ExecContext(ctx,
				fmt.Sprintf(`UPDATE kg_relations SET %s = ? WHERE id IN (%s)`, col, ph),
				updArgs...,
			); err != nil {
				return fmt.Errorf("kg.merge: re-point %s chunk: %w", col, err)
			}
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM kg_entities WHERE id = ?`, sourceID); err != nil {
		return fmt.Errorf("kg.merge: delete source: %w", err)
	}

	// Mark any dedup candidates referencing source as merged
	if _, err := tx.ExecContext(ctx, `
		UPDATE kg_dedup_candidates SET status = 'merged'
		WHERE (entity_a_id = ? OR entity_b_id = ?) AND status = 'pending'`,
		sourceID, sourceID,
	); err != nil {
		slog.Warn("kg.merge: update candidates failed", "error", err)
	}

	return tx.Commit()
}

// DismissCandidate marks a dedup candidate as dismissed.
// Scoped by agent_id to prevent cross-agent dismissal.
func (s *SQLiteKnowledgeGraphStore) DismissCandidate(ctx context.Context, agentID, candidateID string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE kg_dedup_candidates SET status = 'dismissed'
		 WHERE id = ? AND agent_id = ? AND status = 'pending'`,
		candidateID, agentID,
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

// insertDedupCandidate inserts a dedup candidate pair (smaller ID first for dedup consistency).
func (s *SQLiteKnowledgeGraphStore) insertDedupCandidate(ctx context.Context, agentID, userID, entityAID, entityBID string, similarity float64) error {
	if entityAID > entityBID {
		entityAID, entityBID = entityBID, entityAID
	}
	id := uuid.Must(uuid.NewV7()).String()
	tid := tenantIDForInsert(ctx).String()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO kg_dedup_candidates
			(id, tenant_id, agent_id, user_id, entity_a_id, entity_b_id, similarity, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(entity_a_id, entity_b_id) DO NOTHING`,
		id, tid, agentID, userID, entityAID, entityBID, similarity, now,
	)
	return err
}

// fetchEntitiesByIDs loads a set of entities by their DB IDs within an agent scope.
func (s *SQLiteKnowledgeGraphStore) fetchEntitiesByIDs(ctx context.Context, agentID string, ids []string) ([]store.Entity, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	ph := strings.Repeat("?,", len(ids))
	ph = ph[:len(ph)-1]
	args := []any{agentID}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, user_id, external_id, name, entity_type, description,
		        properties, source_id, confidence, created_at, updated_at
		 FROM kg_entities WHERE agent_id = ? AND id IN (`+ph+`)`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEntityRows(rows)
}

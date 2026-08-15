package pg

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
)

// BackfillKGEmbeddings generates embeddings for all KG entities that don't have one yet.
// Processes in batches of 50. Returns total number of entities updated.
// On batch-level embedding failure, skips the batch and continues (up to 3 consecutive failures).
func (s *PGKnowledgeGraphStore) BackfillKGEmbeddings(ctx context.Context) (int, error) {
	if s.embProvider == nil {
		return 0, nil
	}

	const batchSize = 50
	const maxConsecutiveErrors = 3
	total := 0
	consecutiveErrors := 0

	// Backfill is a cross-tenant admin operation — no tenant scoping.
	// context.Background() is typically passed here (no tenant in context).
	batchQ := `SELECT id, name, description FROM kg_entities
		 WHERE embedding IS NULL
		 ORDER BY created_at DESC
		 LIMIT $1`

	// Track failed entity IDs to avoid re-fetching them
	failedIDs := make(map[uuid.UUID]bool)

	for {
		queryArgs := []any{batchSize}
		rows, err := s.db.QueryContext(ctx, batchQ, queryArgs...)
		if err != nil {
			return total, err
		}

		type entityRow struct {
			id   uuid.UUID
			text string
		}
		var pending []entityRow
		for rows.Next() {
			var id uuid.UUID
			var name, desc string
			if err := rows.Scan(&id, &name, &desc); err != nil {
				continue
			}
			if failedIDs[id] {
				continue // skip previously failed entities
			}
			pending = append(pending, entityRow{id: id, text: name + " " + desc})
		}
		rows.Close()

		if len(pending) == 0 {
			break
		}

		slog.Info("backfilling KG entity embeddings", "batch", len(pending), "total_so_far", total)

		texts := make([]string, len(pending))
		for i, p := range pending {
			texts[i] = p.text
		}
		embeddings, err := s.embProvider.Embed(ctx, texts)
		if err != nil {
			slog.Warn("kg entity embedding batch failed, skipping batch", "error", err, "batch_size", len(pending))
			// Mark these entities as failed so we don't re-fetch them
			for _, p := range pending {
				failedIDs[p.id] = true
			}
			consecutiveErrors++
			if consecutiveErrors >= maxConsecutiveErrors {
				slog.Warn("kg backfill: too many consecutive errors, stopping", "errors", consecutiveErrors)
				break
			}
			continue
		}
		consecutiveErrors = 0 // reset on success

		for i, emb := range embeddings {
			if len(emb) == 0 {
				continue
			}
			vecStr := vectorToString(emb)
			if _, err := s.db.ExecContext(ctx,
				`UPDATE kg_entities SET embedding = $1::vector WHERE id = $2`,
				vecStr, pending[i].id,
			); err != nil {
				slog.Warn("kg entity embedding update failed", "entity_id", pending[i].id, "error", err)
				continue
			}
			total++
		}

		if len(pending) < batchSize {
			break
		}
	}

	if total > 0 {
		slog.Info("KG entity embeddings backfill complete", "updated", total)
	}
	return total, nil
}

// EmbedEntity generates and stores an embedding for a single entity.
// Called by UpsertEntity to ensure entities created via HTTP API also get embeddings.
func (s *PGKnowledgeGraphStore) EmbedEntity(ctx context.Context, entityID, name, description string) {
	if s.embProvider == nil {
		return
	}
	// Fail fast on a bad entity UUID so the UPDATE never degrades into a
	// silent no-op WHERE id = uuid.Nil. Callers today pass a freshly minted
	// UUID, but the error path guards against future drift.
	eid, err := parseUUID(entityID)
	if err != nil {
		slog.Warn("kg entity embedding: invalid UUID", "entity_id", entityID, "error", err)
		return
	}
	text := name + " " + description
	embeddings, err := s.embProvider.Embed(ctx, []string{text})
	if err != nil || len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return // best-effort, don't fail the upsert
	}
	vecStr := vectorToString(embeddings[0])
	if _, err := s.db.ExecContext(ctx,
		`UPDATE kg_entities SET embedding = $1::vector WHERE id = $2`,
		vecStr, eid,
	); err != nil {
		slog.Warn("kg entity embedding failed", "entity_id", entityID, "error", err)
	}
}

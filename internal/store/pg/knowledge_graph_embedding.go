package pg

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
)

// BackfillKGEmbeddings generates embeddings for all KG entities that don't have one yet.
// Processes in batches of 50. Returns total number of entities updated.
func (s *PGKnowledgeGraphStore) BackfillKGEmbeddings(ctx context.Context) (int, error) {
	if s.embProvider == nil {
		return 0, nil
	}

	const batchSize = 50
	total := 0

	for {
		rows, err := s.db.QueryContext(ctx,
			`SELECT id, name, description FROM kg_entities
			 WHERE embedding IS NULL
			 ORDER BY created_at DESC
			 LIMIT $1`, batchSize)
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
			slog.Warn("kg entity embedding batch failed", "error", err)
			break
		}

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

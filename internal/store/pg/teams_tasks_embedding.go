package pg

import (
	"cmp"
	"context"
	"log/slog"
	"slices"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// generateTaskEmbedding creates an embedding for a task's subject and stores it.
// Fire-and-forget: logs warnings on error, never blocks the caller.
func (s *PGTeamStore) generateTaskEmbedding(ctx context.Context, taskID uuid.UUID, subject string) {
	if s.embProvider == nil || subject == "" {
		return
	}
	embeddings, err := s.embProvider.Embed(ctx, []string{subject})
	if err != nil {
		slog.Warn("task embedding generation failed", "task_id", taskID, "error", err)
		return
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return
	}
	vecStr := vectorToString(embeddings[0])
	if _, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET embedding = $1::vector WHERE id = $2`, vecStr, taskID,
	); err != nil {
		slog.Warn("task embedding store failed", "task_id", taskID, "error", err)
	}
}

// BackfillTaskEmbeddings generates embeddings for all tasks that don't have one yet.
func (s *PGTeamStore) BackfillTaskEmbeddings(ctx context.Context) (int, error) {
	if s.embProvider == nil {
		return 0, nil
	}

	const batchSize = 50
	total := 0

	for {
		rows, err := s.db.QueryContext(ctx,
			`SELECT id, subject FROM team_tasks
			 WHERE embedding IS NULL AND status NOT IN ('cancelled')
			 ORDER BY created_at DESC
			 LIMIT $1`, batchSize)
		if err != nil {
			return total, err
		}

		type taskRow struct {
			id      uuid.UUID
			subject string
		}
		var pending []taskRow
		for rows.Next() {
			var r taskRow
			if err := rows.Scan(&r.id, &r.subject); err != nil {
				continue
			}
			pending = append(pending, r)
		}
		rows.Close()

		if len(pending) == 0 {
			break
		}

		slog.Info("backfilling task embeddings", "batch", len(pending), "total_so_far", total)

		// Batch embed all subjects at once.
		texts := make([]string, len(pending))
		for i, p := range pending {
			texts[i] = p.subject
		}
		embeddings, err := s.embProvider.Embed(ctx, texts)
		if err != nil {
			slog.Warn("task embedding batch failed", "error", err)
			break
		}

		for i, emb := range embeddings {
			if len(emb) == 0 {
				continue
			}
			vecStr := vectorToString(emb)
			if _, err := s.db.ExecContext(ctx,
				`UPDATE team_tasks SET embedding = $1::vector WHERE id = $2`,
				vecStr, pending[i].id,
			); err != nil {
				slog.Warn("task embedding update failed", "task_id", pending[i].id, "error", err)
				continue
			}
			total++
		}

		if len(pending) < batchSize {
			break
		}
	}

	if total > 0 {
		slog.Info("task embeddings backfill complete", "updated", total)
	}
	return total, nil
}

// SearchTasksByEmbedding performs vector similarity search over team tasks using pgvector cosine distance.
func (s *PGTeamStore) SearchTasksByEmbedding(ctx context.Context, teamID uuid.UUID, embedding []float32, limit int, userID string) ([]store.TeamTaskData, error) {
	if limit <= 0 {
		limit = 5
	}
	vecStr := vectorToString(embedding)

	rows, err := s.db.QueryContext(ctx,
		`SELECT `+taskSelectCols+`
		 `+taskJoinClause+`
		 WHERE t.team_id = $1 AND t.embedding IS NOT NULL
		   AND ($4 = '' OR t.user_id = $4)
		 ORDER BY t.embedding <=> $2::vector
		 LIMIT $3`,
		teamID, vecStr, limit, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTaskRowsJoined(rows)
}

// hybridMergeTaskResults merges FTS and vector search results by task ID.
// FTS results get textWeight, vector results get vecWeight. Duplicates are combined.
func hybridMergeTaskResults(ftsResults, vecResults []store.TeamTaskData, textWeight, vecWeight float64, limit int) []store.TeamTaskData {
	type scored struct {
		task     store.TeamTaskData
		ftsRank  float64
		vecRank  float64
		combined float64
	}

	byID := make(map[uuid.UUID]*scored)

	// FTS results: rank by position (first = best).
	for i, t := range ftsResults {
		rank := 1.0 - float64(i)/float64(max(len(ftsResults), 1))
		byID[t.ID] = &scored{task: t, ftsRank: rank}
	}

	// Vector results: rank by position (first = best cosine similarity).
	for i, t := range vecResults {
		rank := 1.0 - float64(i)/float64(max(len(vecResults), 1))
		if s, ok := byID[t.ID]; ok {
			s.vecRank = rank
		} else {
			byID[t.ID] = &scored{task: t, vecRank: rank}
		}
	}

	// Compute combined scores.
	results := make([]scored, 0, len(byID))
	for _, s := range byID {
		s.combined = s.ftsRank*textWeight + s.vecRank*vecWeight
		results = append(results, *s)
	}

	// Sort by combined score descending.
	slices.SortFunc(results, func(a, b scored) int {
		return cmp.Compare(b.combined, a.combined) // descending
	})

	if len(results) > limit {
		results = results[:limit]
	}

	out := make([]store.TeamTaskData, len(results))
	for i, s := range results {
		out[i] = s.task
	}
	return out
}

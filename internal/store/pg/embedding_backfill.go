package pg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const embeddingBackfillBatchSize = 50

type embeddingBackfillItem[T any] struct {
	row  T
	id   uuid.UUID
	text string
}

// processEmbeddingBackfillBatch uses an efficient batch request first, then
// isolates individual rows if the provider rejects or truncates the batch. A
// malformed row remains retryable without starving every later row forever.
func processEmbeddingBackfillBatch[T any](
	ctx context.Context,
	provider store.EmbeddingProvider,
	surface string,
	items []embeddingBackfillItem[T],
	update func(context.Context, T, []float32) (int64, error),
) (int, error) {
	texts := make([]string, len(items))
	for i, item := range items {
		texts[i] = item.text
	}

	embeddings, batchErr := provider.Embed(ctx, texts)
	if batchErr == nil {
		batchErr = validateEmbeddingBatch(embeddings, len(items))
	}
	if batchErr == nil {
		return updateEmbeddingBackfillItems(ctx, surface, items, embeddings, update)
	}
	if ctx.Err() != nil {
		return 0, fmt.Errorf("generate %s embeddings: %w", surface, ctx.Err())
	}

	slog.Warn("embedding backfill batch failed; retrying rows individually",
		"surface", surface, "rows", len(items), "error", batchErr)
	total := 0
	var rowErrs []error
	for _, item := range items {
		embedding, err := provider.Embed(ctx, []string{item.text})
		if err == nil {
			err = validateEmbeddingBatch(embedding, 1)
		}
		if err != nil {
			rowErrs = append(rowErrs, fmt.Errorf("generate %s embedding id=%s: %w", surface, item.id, err))
			if ctx.Err() != nil {
				break
			}
			continue
		}
		updated, err := update(ctx, item.row, embedding[0])
		if err != nil {
			rowErrs = append(rowErrs, fmt.Errorf("update %s embedding id=%s: %w", surface, item.id, err))
			continue
		}
		total += int(updated)
	}
	return total, errors.Join(rowErrs...)
}

func validateEmbeddingBatch(embeddings [][]float32, expected int) error {
	if len(embeddings) != expected {
		return fmt.Errorf("got %d vectors for %d inputs", len(embeddings), expected)
	}
	for i, embedding := range embeddings {
		if len(embedding) == 0 {
			return fmt.Errorf("empty vector at index %d", i)
		}
	}
	return nil
}

func updateEmbeddingBackfillItems[T any](
	ctx context.Context,
	surface string,
	items []embeddingBackfillItem[T],
	embeddings [][]float32,
	update func(context.Context, T, []float32) (int64, error),
) (int, error) {
	total := 0
	var updateErrs []error
	for i, item := range items {
		updated, err := update(ctx, item.row, embeddings[i])
		if err != nil {
			updateErrs = append(updateErrs, fmt.Errorf("update %s embedding id=%s: %w", surface, item.id, err))
			continue
		}
		total += int(updated)
	}
	return total, errors.Join(updateErrs...)
}

// BackfillVaultEmbeddings generates vectors for vault documents that were
// persisted while the embedding provider was unavailable. Documents without a
// summary still get a title/path vector; enrichment can later replace it with a
// richer title/path/summary vector.
func (s *PGVaultStore) BackfillVaultEmbeddings(ctx context.Context) (int, error) {
	if s.embProvider == nil {
		return 0, fmt.Errorf("no embedding provider configured")
	}

	type vaultBackfillRow struct {
		id      uuid.UUID
		title   string
		path    string
		summary string
	}

	total := 0
	cursor := uuid.Nil
	var backfillErrs []error
	for {
		rows, err := s.db.QueryContext(ctx, `
			SELECT id, title, path, COALESCE(summary, '')
			FROM vault_documents
			WHERE embedding IS NULL AND id > $1
			ORDER BY id ASC
			LIMIT $2`, cursor, embeddingBackfillBatchSize)
		if err != nil {
			return total, fmt.Errorf("query vault documents without embeddings: %w", err)
		}

		pending := make([]vaultBackfillRow, 0, embeddingBackfillBatchSize)
		for rows.Next() {
			var row vaultBackfillRow
			if err := rows.Scan(&row.id, &row.title, &row.path, &row.summary); err != nil {
				rows.Close()
				return total, fmt.Errorf("scan vault document for embedding backfill: %w", err)
			}
			pending = append(pending, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return total, fmt.Errorf("iterate vault documents for embedding backfill: %w", err)
		}
		if err := rows.Close(); err != nil {
			return total, fmt.Errorf("close vault embedding backfill rows: %w", err)
		}
		if len(pending) == 0 {
			return total, errors.Join(backfillErrs...)
		}

		items := make([]embeddingBackfillItem[vaultBackfillRow], len(pending))
		for i, doc := range pending {
			items[i] = embeddingBackfillItem[vaultBackfillRow]{
				row:  doc,
				id:   doc.id,
				text: doc.title + " " + doc.path + " " + doc.summary,
			}
		}
		updated, err := processEmbeddingBackfillBatch(ctx, s.embProvider, "vault document", items,
			func(ctx context.Context, doc vaultBackfillRow, embedding []float32) (int64, error) {
				result, err := s.db.ExecContext(ctx, `
					UPDATE vault_documents SET embedding = $1::vector
					WHERE id = $2 AND embedding IS NULL
					  AND title = $3 AND path = $4 AND COALESCE(summary, '') = $5`,
					vectorToString(embedding), doc.id, doc.title, doc.path, doc.summary)
				if err != nil {
					return 0, err
				}
				return result.RowsAffected()
			})
		total += updated
		if err != nil {
			backfillErrs = append(backfillErrs, err)
			if ctx.Err() != nil {
				return total, errors.Join(backfillErrs...)
			}
		}
		cursor = pending[len(pending)-1].id
	}
}

// BackfillEpisodicEmbeddings generates vectors for unexpired summaries that
// were persisted while the embedding provider was unavailable.
func (s *PGEpisodicStore) BackfillEpisodicEmbeddings(ctx context.Context) (int, error) {
	if s.embProvider == nil {
		return 0, fmt.Errorf("no embedding provider configured")
	}

	type episodicBackfillRow struct {
		id      uuid.UUID
		summary string
	}

	total := 0
	cursor := uuid.Nil
	var backfillErrs []error
	for {
		rows, err := s.db.QueryContext(ctx, `
			SELECT id, summary
			FROM episodic_summaries
			WHERE embedding IS NULL AND summary != '' AND id > $1
			  AND (expires_at IS NULL OR expires_at > NOW())
			ORDER BY id ASC
			LIMIT $2`, cursor, embeddingBackfillBatchSize)
		if err != nil {
			return total, fmt.Errorf("query episodic summaries without embeddings: %w", err)
		}

		pending := make([]episodicBackfillRow, 0, embeddingBackfillBatchSize)
		for rows.Next() {
			var row episodicBackfillRow
			if err := rows.Scan(&row.id, &row.summary); err != nil {
				rows.Close()
				return total, fmt.Errorf("scan episodic summary for embedding backfill: %w", err)
			}
			pending = append(pending, row)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return total, fmt.Errorf("iterate episodic summaries for embedding backfill: %w", err)
		}
		if err := rows.Close(); err != nil {
			return total, fmt.Errorf("close episodic embedding backfill rows: %w", err)
		}
		if len(pending) == 0 {
			return total, errors.Join(backfillErrs...)
		}

		items := make([]embeddingBackfillItem[episodicBackfillRow], len(pending))
		for i, summary := range pending {
			items[i] = embeddingBackfillItem[episodicBackfillRow]{
				row:  summary,
				id:   summary.id,
				text: summary.summary,
			}
		}
		updated, err := processEmbeddingBackfillBatch(ctx, s.embProvider, "episodic summary", items,
			func(ctx context.Context, summary episodicBackfillRow, embedding []float32) (int64, error) {
				result, err := s.db.ExecContext(ctx, `
					UPDATE episodic_summaries SET embedding = $1::vector
					WHERE id = $2 AND embedding IS NULL AND summary = $3
					  AND (expires_at IS NULL OR expires_at > NOW())`,
					vectorToString(embedding), summary.id, summary.summary)
				if err != nil {
					return 0, err
				}
				return result.RowsAffected()
			})
		total += updated
		if err != nil {
			backfillErrs = append(backfillErrs, err)
			if ctx.Err() != nil {
				return total, errors.Join(backfillErrs...)
			}
		}
		cursor = pending[len(pending)-1].id
	}
}

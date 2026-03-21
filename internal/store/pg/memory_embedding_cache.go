package pg

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// embeddingCacheEntry holds data for a single cache row.
type embeddingCacheEntry struct {
	Hash      string
	Embedding []float32
}

// lookupEmbeddingCache fetches cached embeddings for the given content hashes.
// Returns a map from hash -> embedding vector. Missing hashes are simply absent.
func (s *PGMemoryStore) lookupEmbeddingCache(ctx context.Context, hashes []string, provider, model string) (map[string][]float32, error) {
	if len(hashes) == 0 {
		return nil, nil
	}

	// Build positional params: $1..$N for hashes, $N+1 for provider, $N+2 for model
	placeholders := make([]string, len(hashes))
	args := make([]any, 0, len(hashes)+2)
	for i, h := range hashes {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args = append(args, h)
	}
	args = append(args, provider, model)

	query := fmt.Sprintf(
		"SELECT hash, embedding FROM embedding_cache WHERE hash IN (%s) AND provider = $%d AND model = $%d",
		strings.Join(placeholders, ","), len(hashes)+1, len(hashes)+2,
	)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("lookup embedding cache: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]float32, len(hashes))
	for rows.Next() {
		var hash, vecStr string
		if err := rows.Scan(&hash, &vecStr); err != nil {
			slog.Warn("embedding cache scan error", "error", err)
			continue
		}
		vec, err := parseVector(vecStr)
		if err != nil {
			slog.Warn("embedding cache parse error", "hash", hash, "error", err)
			continue
		}
		result[hash] = vec
	}
	return result, rows.Err()
}

// writeEmbeddingCache batch-upserts embedding cache entries.
// Gracefully skips on dimension mismatch (schema uses vector(1536)).
func (s *PGMemoryStore) writeEmbeddingCache(ctx context.Context, entries []embeddingCacheEntry, provider, model string) error {
	if len(entries) == 0 {
		return nil
	}

	now := time.Now()

	// Process in batches of 100 to avoid exceeding max query params
	const batchSize = 100
	for start := 0; start < len(entries); start += batchSize {
		end := start + batchSize
		if end > len(entries) {
			end = len(entries)
		}
		batch := entries[start:end]

		var sb strings.Builder
		sb.WriteString(`INSERT INTO embedding_cache (hash, provider, model, embedding, dims, created_at, updated_at) VALUES `)
		args := make([]any, 0, len(batch)*6)
		for i, e := range batch {
			if i > 0 {
				sb.WriteByte(',')
			}
			base := i * 6
			fmt.Fprintf(&sb, "($%d,$%d,$%d,$%d::vector,$%d,$%d,$%d)",
				base+1, base+2, base+3, base+4, base+5, base+6, base+6)
			args = append(args, e.Hash, provider, model, vectorToString(e.Embedding), len(e.Embedding), now)
		}
		sb.WriteString(` ON CONFLICT (hash, provider, model) DO UPDATE SET embedding = EXCLUDED.embedding, dims = EXCLUDED.dims, updated_at = EXCLUDED.updated_at`)

		_, err := s.db.ExecContext(ctx, sb.String(), args...)
		if err != nil {
			// pgvector dimension mismatch — skip cache gracefully
			if strings.Contains(err.Error(), "dimensions") {
				slog.Warn("embedding cache skipped: vector dimension mismatch",
					"provider", provider, "model", model,
					"actual_dims", len(batch[0].Embedding), "error", err)
				return nil
			}
			return fmt.Errorf("batch write embedding cache: %w", err)
		}
	}
	return nil
}

// parseVector converts a pgvector string like "[0.1,0.2,0.3]" into []float32.
func parseVector(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return nil, fmt.Errorf("vector string too short: %q", s)
	}
	// Strip surrounding brackets ([] from pgvector, () as fallback)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	s = strings.TrimPrefix(s, "(")
	s = strings.TrimSuffix(s, ")")
	if s == "" {
		return nil, nil
	}

	parts := strings.Split(s, ",")
	vec := make([]float32, 0, len(parts))
	for _, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("parse vector element %q: %w", p, err)
		}
		vec = append(vec, float32(f))
	}
	return vec, nil
}

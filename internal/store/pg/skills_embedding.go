package pg

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// SetEmbeddingProvider sets the embedding provider for vector-based skill search.
func (s *PGSkillStore) SetEmbeddingProvider(provider store.EmbeddingProvider) {
	s.embProvider = provider
}

// SearchByEmbedding performs vector similarity search over skills using pgvector cosine distance.
func (s *PGSkillStore) SearchByEmbedding(ctx context.Context, embedding []float32, limit int) ([]store.SkillSearchResult, error) {
	if limit <= 0 {
		limit = 5
	}
	vecStr := vectorToString(embedding)

	// $1=vec, scope starts at $2 (if present), ORDER vec uses next available param, LIMIT after.
	tc, tcArgs, nextParam, err := scopeClause(ctx, 2)
	if err != nil {
		return nil, err
	}
	tenantCond := buildSkillEmbeddingTenantCond(tc)
	orderN := nextParam
	limitN := orderN + 1
	q := fmt.Sprintf(`SELECT name, slug, COALESCE(description, ''), version, file_path,
			1 - (embedding <=> $1::vector) AS score
		FROM skills
		WHERE status = 'active' AND enabled = true AND embedding IS NOT NULL
		  AND visibility != 'private'%s
		ORDER BY embedding <=> $%d::vector
		LIMIT $%d`, tenantCond, orderN, limitN)

	args := append([]any{vecStr}, tcArgs...)
	args = append(args, vecStr, limit)

	var scanned []skillEmbeddingSearchRow
	if err := pkgSqlxDB.SelectContext(ctx, &scanned, q, args...); err != nil {
		return nil, fmt.Errorf("embedding skill search: %w", err)
	}

	results := make([]store.SkillSearchResult, 0, len(scanned))
	for _, row := range scanned {
		r := store.SkillSearchResult{
			Name:        row.Name,
			Slug:        row.Slug,
			Description: row.Desc,
			Score:       row.Score,
		}
		// Use DB file_path when available; fall back to baseDir construction.
		if row.FilePath != nil && *row.FilePath != "" {
			r.Path = *row.FilePath + "/SKILL.md"
		} else {
			r.Path = fmt.Sprintf("%s/%s/%d/SKILL.md", s.baseDir, row.Slug, row.Version)
		}
		results = append(results, r)
	}
	return results, nil
}


func buildSkillEmbeddingTenantCond(scope string) string {
	if scope == "" {
		return ""
	}
	tenantExpr := strings.TrimPrefix(scope, " AND ")
	return fmt.Sprintf(" AND (is_system = true OR (%s))", tenantExpr)
}

// BackfillSkillEmbeddings generates embeddings for all active skills that don't have one yet.
func (s *PGSkillStore) BackfillSkillEmbeddings(ctx context.Context) (int, error) {
	if s.embProvider == nil {
		return 0, nil
	}

	var pending []skillBackfillRow
	if err := pkgSqlxDB.SelectContext(ctx, &pending,
		`SELECT id, name, COALESCE(description, '') AS description FROM skills WHERE status = 'active' AND enabled = true AND embedding IS NULL`,
	); err != nil {
		return 0, err
	}

	if len(pending) == 0 {
		return 0, nil
	}

	slog.Info("backfilling skill embeddings", "count", len(pending))
	updated := 0
	for _, sk := range pending {
		text := sk.Name
		if sk.Desc != "" {
			text += ": " + sk.Desc
		}
		embeddings, err := s.embProvider.Embed(ctx, []string{text})
		if err != nil {
			slog.Warn("skill embedding failed", "skill", sk.Name, "error", err)
			continue
		}
		if len(embeddings) == 0 || len(embeddings[0]) == 0 {
			continue
		}
		vecStr := vectorToString(embeddings[0])
		_, err = s.db.ExecContext(ctx,
			`UPDATE skills SET embedding = $1::vector WHERE id = $2`, vecStr, sk.ID)
		if err != nil {
			slog.Warn("skill embedding update failed", "skill", sk.Name, "error", err)
			continue
		}
		updated++
	}

	slog.Info("skill embeddings backfill complete", "updated", updated)
	return updated, nil
}

// generateEmbedding creates an embedding for a skill's name+description and stores it.
func (s *PGSkillStore) generateEmbedding(ctx context.Context, slug, name, description string) {
	if s.embProvider == nil {
		return
	}
	text := name
	if description != "" {
		text += ": " + description
	}
	embeddings, err := s.embProvider.Embed(ctx, []string{text})
	if err != nil {
		slog.Warn("skill embedding generation failed", "skill", name, "error", err)
		return
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return
	}
	vecStr := vectorToString(embeddings[0])
	_, err = s.db.ExecContext(ctx,
		`UPDATE skills SET embedding = $1::vector WHERE slug = $2 AND status = 'active'`, vecStr, slug)
	if err != nil {
		slog.Warn("skill embedding store failed", "skill", name, "error", err)
	}
}

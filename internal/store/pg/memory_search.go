package pg

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Search performs hybrid search (FTS + vector) over memory_chunks.
// Merges global (user_id IS NULL) + per-user chunks, with user boost.
func (s *PGMemoryStore) Search(ctx context.Context, query string, agentID, userID string, opts store.MemorySearchOptions) ([]store.MemorySearchResult, error) {
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = s.cfg.MaxResults
	}

	aid := mustParseUUID(agentID)

	// FTS search using tsvector
	ftsResults, err := s.ftsSearch(ctx, query, aid, userID, maxResults*2)
	if err != nil {
		return nil, err
	}

	// Vector search if provider available
	var vecResults []scoredChunk
	if s.provider != nil {
		embeddings, err := s.provider.Embed(ctx, []string{query})
		if err == nil && len(embeddings) > 0 {
			vecResults, err = s.vectorSearch(ctx, embeddings[0], aid, userID, maxResults*2)
			if err != nil {
				vecResults = nil
			}
		}
	}

	// Merge results — use per-query overrides if set, else store defaults
	textW, vecW := s.cfg.TextWeight, s.cfg.VectorWeight
	if opts.TextWeight > 0 {
		textW = opts.TextWeight
	}
	if opts.VectorWeight > 0 {
		vecW = opts.VectorWeight
	}
	if len(ftsResults) == 0 && len(vecResults) > 0 {
		textW, vecW = 0, 1.0
	} else if len(vecResults) == 0 && len(ftsResults) > 0 {
		textW, vecW = 1.0, 0
	}
	merged := hybridMerge(ftsResults, vecResults, textW, vecW, userID)

	// Apply min score filter
	var filtered []store.MemorySearchResult
	for _, m := range merged {
		if opts.MinScore > 0 && m.Score < opts.MinScore {
			continue
		}
		if opts.PathPrefix != "" && len(m.Path) < len(opts.PathPrefix) {
			continue
		}
		filtered = append(filtered, m)
		if len(filtered) >= maxResults {
			break
		}
	}

	return filtered, nil
}

type scoredChunk struct {
	Path      string
	StartLine int
	EndLine   int
	Text      string
	Score     float64
	UserID    *string
}

func (s *PGMemoryStore) ftsSearch(ctx context.Context, query string, agentID any, userID string, limit int) ([]scoredChunk, error) {
	var q string
	var args []any

	if store.IsSharedMemory(ctx) {
		// Shared: no user_id filter — search ALL chunks for agent
		tc, tcArgs, _, err := scopeClause(ctx, 4)
		if err != nil {
			return nil, err
		}
		limitN := 4 + len(tcArgs)
		q = fmt.Sprintf(`SELECT path, start_line, end_line, text, user_id,
				ts_rank(tsv, plainto_tsquery('simple', $1)) AS score
			FROM memory_chunks
			WHERE agent_id = $2 AND tsv @@ plainto_tsquery('simple', $3)%s
			ORDER BY score DESC LIMIT $%d`, tc, limitN)
		args = append([]any{query, agentID, query}, tcArgs...)
		args = append(args, limit)
	} else if userID != "" {
		// fixed params: $1=query, $2=agentID, $3=query, $4=userID
		tc, tcArgs, _, err := scopeClause(ctx, 5)
		if err != nil {
			return nil, err
		}
		limitN := 5 + len(tcArgs)
		q = fmt.Sprintf(`SELECT path, start_line, end_line, text, user_id,
				ts_rank(tsv, plainto_tsquery('simple', $1)) AS score
			FROM memory_chunks
			WHERE agent_id = $2 AND tsv @@ plainto_tsquery('simple', $3)
			AND (user_id IS NULL OR user_id = $4)%s
			ORDER BY score DESC LIMIT $%d`, tc, limitN)
		args = append([]any{query, agentID, query, userID}, tcArgs...)
		args = append(args, limit)
	} else {
		// fixed params: $1=query, $2=agentID, $3=query
		tc, tcArgs, _, err := scopeClause(ctx, 4)
		if err != nil {
			return nil, err
		}
		limitN := 4 + len(tcArgs)
		q = fmt.Sprintf(`SELECT path, start_line, end_line, text, user_id,
				ts_rank(tsv, plainto_tsquery('simple', $1)) AS score
			FROM memory_chunks
			WHERE agent_id = $2 AND tsv @@ plainto_tsquery('simple', $3)
			AND user_id IS NULL%s
			ORDER BY score DESC LIMIT $%d`, tc, limitN)
		args = append([]any{query, agentID, query}, tcArgs...)
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []scoredChunk
	for rows.Next() {
		var r scoredChunk
		rows.Scan(&r.Path, &r.StartLine, &r.EndLine, &r.Text, &r.UserID, &r.Score)
		results = append(results, r)
	}
	return results, nil
}

func (s *PGMemoryStore) vectorSearch(ctx context.Context, embedding []float32, agentID any, userID string, limit int) ([]scoredChunk, error) {
	vecStr := vectorToString(embedding)

	var q string
	var args []any

	if store.IsSharedMemory(ctx) {
		// Shared: no user_id filter — search ALL chunks for agent
		tc, tcArgs, _, err := scopeClause(ctx, 3)
		if err != nil {
			return nil, err
		}
		orderN := 3 + len(tcArgs)
		limitN := orderN + 1
		q = fmt.Sprintf(`SELECT path, start_line, end_line, text, user_id,
				1 - (embedding <=> $1::vector) AS score
			FROM memory_chunks
			WHERE agent_id = $2 AND embedding IS NOT NULL%s
			ORDER BY embedding <=> $%d::vector LIMIT $%d`, tc, orderN, limitN)
		args = append([]any{vecStr, agentID}, tcArgs...)
		args = append(args, vecStr, limit)
	} else if userID != "" {
		// fixed params: $1=vec, $2=agentID, $3=userID
		tc, tcArgs, _, err := scopeClause(ctx, 4)
		if err != nil {
			return nil, err
		}
		orderN := 4 + len(tcArgs)
		limitN := orderN + 1
		q = fmt.Sprintf(`SELECT path, start_line, end_line, text, user_id,
				1 - (embedding <=> $1::vector) AS score
			FROM memory_chunks
			WHERE agent_id = $2 AND embedding IS NOT NULL
			AND (user_id IS NULL OR user_id = $3)%s
			ORDER BY embedding <=> $%d::vector LIMIT $%d`, tc, orderN, limitN)
		args = append([]any{vecStr, agentID, userID}, tcArgs...)
		args = append(args, vecStr, limit)
	} else {
		// fixed params: $1=vec, $2=agentID
		tc, tcArgs, _, err := scopeClause(ctx, 3)
		if err != nil {
			return nil, err
		}
		orderN := 3 + len(tcArgs)
		limitN := orderN + 1
		q = fmt.Sprintf(`SELECT path, start_line, end_line, text, user_id,
				1 - (embedding <=> $1::vector) AS score
			FROM memory_chunks
			WHERE agent_id = $2 AND embedding IS NOT NULL
			AND user_id IS NULL%s
			ORDER BY embedding <=> $%d::vector LIMIT $%d`, tc, orderN, limitN)
		args = append([]any{vecStr, agentID}, tcArgs...)
		args = append(args, vecStr, limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []scoredChunk
	for rows.Next() {
		var r scoredChunk
		rows.Scan(&r.Path, &r.StartLine, &r.EndLine, &r.Text, &r.UserID, &r.Score)
		results = append(results, r)
	}
	return results, nil
}

// hybridMerge combines FTS and vector results with weighted scoring.
// Per-user results get a 1.2x boost. Deduplication: user copy wins over global.
// NOTE: when shared memory is active, the 1.2x personal boost still applies —
// consider removing it in shared mode if all docs should be treated equally.
func hybridMerge(fts, vec []scoredChunk, textWeight, vectorWeight float64, currentUserID string) []store.MemorySearchResult {
	type key struct {
		Path      string
		StartLine int
	}
	seen := make(map[key]*store.MemorySearchResult)

	addResult := func(r scoredChunk, weight float64) {
		k := key{r.Path, r.StartLine}
		scope := "global"
		boost := 1.0
		if r.UserID != nil && *r.UserID != "" {
			scope = "personal"
			boost = 1.2
		}
		score := r.Score * weight * boost

		if existing, ok := seen[k]; ok {
			existing.Score += score
			// User copy wins
			if scope == "personal" {
				existing.Scope = "personal"
				existing.Snippet = r.Text
			}
		} else {
			seen[k] = &store.MemorySearchResult{
				Path:      r.Path,
				StartLine: r.StartLine,
				EndLine:   r.EndLine,
				Score:     score,
				Snippet:   r.Text,
				Source:    "memory",
				Scope:     scope,
			}
		}
	}

	for _, r := range fts {
		addResult(r, textWeight)
	}
	for _, r := range vec {
		addResult(r, vectorWeight)
	}

	// Collect and sort by score
	results := make([]store.MemorySearchResult, 0, len(seen))
	for _, r := range seen {
		results = append(results, *r)
	}

	// Simple sort (descending score)
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	return results
}


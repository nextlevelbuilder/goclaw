//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Search performs LIKE-based text search over memory_chunks.
// Vector search is not available in the SQLite edition — VectorSearch is always false.
// Merges global (user_id IS NULL) + per-user chunks, with user boost.
func (s *SQLiteMemoryStore) Search(ctx context.Context, query string, agentID, userID string, opts store.MemorySearchOptions) ([]store.MemorySearchResult, error) {
	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = s.cfg.MaxResults
	}

	results, err := s.likeSearch(ctx, query, agentID, userID, maxResults*2)
	if err != nil {
		return nil, err
	}

	// Apply filters and cap results
	var filtered []store.MemorySearchResult
	for _, r := range results {
		if opts.MinScore > 0 && r.Score < opts.MinScore {
			continue
		}
		if opts.PathPrefix != "" && !strings.HasPrefix(r.Path, opts.PathPrefix) {
			continue
		}
		filtered = append(filtered, r)
		if len(filtered) >= maxResults {
			break
		}
	}
	return filtered, nil
}

// likeSearch performs a case-insensitive LIKE search across chunk text.
// Returns results scored 1.0 (global) or 1.2 (personal, boosted).
func (s *SQLiteMemoryStore) likeSearch(ctx context.Context, query, agentID, userID string, limit int) ([]store.MemorySearchResult, error) {
	pattern := "%" + escapeLike(query) + "%"

	var q string
	var args []any

	if userID != "" {
		tc, tcArgs, err := scopeClause(ctx)
		if err != nil {
			return nil, err
		}
		q = `SELECT path, start_line, end_line, text, user_id
			 FROM memory_chunks
			 WHERE agent_id = ? AND (user_id IS NULL OR user_id = ?)
			 AND text LIKE ? ESCAPE '\'` + tc + `
			 ORDER BY user_id DESC
			 LIMIT ?`
		args = append([]any{agentID, userID, pattern}, tcArgs...)
		args = append(args, limit)
	} else {
		tc, tcArgs, err := scopeClause(ctx)
		if err != nil {
			return nil, err
		}
		q = `SELECT path, start_line, end_line, text, user_id
			 FROM memory_chunks
			 WHERE agent_id = ? AND user_id IS NULL
			 AND text LIKE ? ESCAPE '\'` + tc + `
			 LIMIT ?`
		args = append([]any{agentID, pattern}, tcArgs...)
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []store.MemorySearchResult
	for rows.Next() {
		var path, text string
		var startLine, endLine int
		var uid *string
		if err := rows.Scan(&path, &startLine, &endLine, &text, &uid); err != nil {
			continue
		}
		scope := "global"
		score := 1.0
		if uid != nil && *uid != "" {
			scope = "personal"
			score = 1.2 // personal boost, mirrors PG hybrid merge
		}
		results = append(results, store.MemorySearchResult{
			Path:      path,
			StartLine: startLine,
			EndLine:   endLine,
			Score:     score,
			Snippet:   text,
			Source:    "memory",
			Scope:     scope,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// escapeLike escapes special LIKE metacharacters: % _ \
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

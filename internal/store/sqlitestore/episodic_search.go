//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Search performs LIKE-based text search over episodic summaries.
// Vector search is not available in the SQLite edition.
// Scoring: 1.0 base, +0.2 if l0_abstract matches, +0.1 if key_topics matches.
func (s *SQLiteEpisodicStore) Search(ctx context.Context, query string, agentID, userID string, opts store.EpisodicSearchOptions) ([]store.EpisodicSearchResult, error) {
	// F10: cap query to prevent degenerate LIKE patterns
	if len(query) > 500 {
		query = query[:500]
	}

	maxResults := opts.MaxResults
	if maxResults <= 0 {
		maxResults = 10
	}

	tenantID := tenantIDForInsert(ctx)
	query = strings.TrimSpace(query)
	listOnly := query == "" || query == "*"

	q := `
		SELECT id, COALESCE(NULLIF(l0_abstract, ''), substr(summary, 1, 500)) AS l0_abstract, key_topics, created_at, expires_at, session_key
		FROM episodic_summaries
		WHERE agent_id = ? AND tenant_id = ?`
	args := []any{agentID, tenantID.String()}
	if !listOnly {
		pattern := "%" + escapeLike(query) + "%"
		q += " AND (summary LIKE ? ESCAPE '\\' OR key_topics LIKE ? ESCAPE '\\')"
		args = append(args, pattern, pattern)
	}
	if store.IsSharedMemory(ctx) {
		// Shared memory searches all user scopes for the agent.
	} else if userID != "" {
		q += " AND (user_id = ? OR user_id = '')"
		args = append(args, userID)
	} else {
		q += " AND user_id = ''"
	}
	if opts.CreatedAfter != nil {
		q += " AND created_at >= ?"
		args = append(args, opts.CreatedAfter.UTC().Format(time.RFC3339Nano))
	}
	if opts.CreatedBefore != nil {
		q += " AND created_at < ?"
		args = append(args, opts.CreatedBefore.UTC().Format(time.RFC3339Nano))
	}
	if !opts.IncludeExpired {
		q += " AND (expires_at IS NULL OR expires_at > ?)"
		args = append(args, time.Now().UTC().Format(time.RFC3339Nano))
	}
	q += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, maxResults*3)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type rawRow struct {
		id         string
		l0Abstract string
		keyTopics  string
		createdAt  sqliteTime
		expiresAt  nullSqliteTime
		sessionKey string
	}

	var raws []rawRow
	for rows.Next() {
		var r rawRow
		if err := rows.Scan(&r.id, &r.l0Abstract, &r.keyTopics, &r.createdAt, &r.expiresAt, &r.sessionKey); err != nil {
			continue
		}
		raws = append(raws, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Post-query scoring
	lowerQuery := strings.ToLower(query)
	type scored struct {
		raw   rawRow
		score float64
	}
	scoredRows := make([]scored, 0, len(raws))
	for _, r := range raws {
		sc := 1.0
		if !listOnly {
			if strings.Contains(strings.ToLower(r.l0Abstract), lowerQuery) {
				sc += 0.2
			}
			if strings.Contains(strings.ToLower(r.keyTopics), lowerQuery) {
				sc += 0.1
			}
		}
		scoredRows = append(scoredRows, scored{raw: r, score: sc})
	}

	// Sort by score DESC, then created_at DESC
	sort.SliceStable(scoredRows, func(i, j int) bool {
		if scoredRows[i].score != scoredRows[j].score {
			return scoredRows[i].score > scoredRows[j].score
		}
		return scoredRows[i].raw.createdAt.Time.After(scoredRows[j].raw.createdAt.Time)
	})

	var results []store.EpisodicSearchResult
	for _, sr := range scoredRows {
		if opts.MinScore > 0 && sr.score < opts.MinScore {
			continue
		}
		var expiresAt *time.Time
		if sr.raw.expiresAt.Valid {
			t := sr.raw.expiresAt.Time
			expiresAt = &t
		}
		results = append(results, store.EpisodicSearchResult{
			EpisodicID: sr.raw.id,
			L0Abstract: sr.raw.l0Abstract,
			KeyTopics:  searchKeyTopics(sr.raw.keyTopics),
			Score:      sr.score,
			CreatedAt:  sr.raw.createdAt.Time,
			ExpiresAt:  expiresAt,
			SessionKey: sr.raw.sessionKey,
		})
		if len(results) >= maxResults {
			break
		}
	}
	return results, nil
}

func searchKeyTopics(raw string) []string {
	var topics []string
	scanJSONStringArray([]byte(raw), &topics)
	return topics
}

// Ensure SQLiteEpisodicStore implements store.EpisodicStore.
var _ store.EpisodicStore = (*SQLiteEpisodicStore)(nil)

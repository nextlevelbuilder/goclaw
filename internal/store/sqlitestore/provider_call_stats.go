//go:build sqlite || sqliteonly

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Shared filter for finished LLM calls of one provider in one tenant and time
// range; args: tenant, provider, from, to. Times bind as time.Time, the same way
// CreateSpan writes start_time, so the text comparison stays consistent.
const providerCallsWhere = `
FROM spans
WHERE tenant_id = ?
  AND provider = ?
  AND span_type = 'llm_call'
  AND status IN ('completed', 'error')
  AND start_time >= ?
  AND start_time < ?`

// GetProviderCallStats implements store.ProviderCallStatsStore. SQLite has no
// percentile function, so the nearest-rank rows are read by offset, which keeps
// memory bounded on long ranges.
func (s *SQLiteTracingStore) GetProviderCallStats(ctx context.Context, q store.ProviderCallStatsQuery) (*store.ProviderCallStats, error) {
	out := &store.ProviderCallStats{RecentErrors: []store.ProviderErrorSpan{}}
	args := []any{q.TenantID, q.Provider, q.From, q.To}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END), 0)`+providerCallsWhere,
		args...).Scan(&out.Calls, &out.Errors); err != nil {
		return nil, err
	}
	if out.Calls == 0 {
		return out, nil
	}

	var err error
	if out.LatencyP50MS, err = s.durationAtRank(ctx, args, store.NearestRank(0.5, out.Calls)); err != nil {
		return nil, err
	}
	if out.LatencyP95MS, err = s.durationAtRank(ctx, args, store.NearestRank(0.95, out.Calls)); err != nil {
		return nil, err
	}

	if out.Errors == 0 || q.ErrorLimit <= 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT start_time, COALESCE(error, '')`+providerCallsWhere+` AND status = 'error' ORDER BY start_time DESC LIMIT ?`,
		append(args, q.ErrorLimit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var at sqliteTime
		var e store.ProviderErrorSpan
		if err := rows.Scan(&at, &e.Error); err != nil {
			return nil, err
		}
		e.At = at.Time
		out.RecentErrors = append(out.RecentErrors, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// durationAtRank returns the duration of the rank-th (1-based) fastest call.
func (s *SQLiteTracingStore) durationAtRank(ctx context.Context, args []any, rank int64) (float64, error) {
	var ms int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(duration_ms, 0)`+providerCallsWhere+` ORDER BY COALESCE(duration_ms, 0) LIMIT 1 OFFSET ?`,
		append(args, rank-1)...).Scan(&ms)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return float64(ms), err
}

var _ store.ProviderCallStatsStore = (*SQLiteTracingStore)(nil)

package pg

import (
	"context"
	"database/sql"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// Finished LLM calls of one provider in one tenant and time range. Running spans
// are left out: they have no duration yet and no outcome.
const providerCallStatsQuery = `
SELECT
	COUNT(*),
	COUNT(*) FILTER (WHERE status = 'error'),
	COALESCE(percentile_disc(0.5) WITHIN GROUP (ORDER BY COALESCE(duration_ms, 0)), 0),
	COALESCE(percentile_disc(0.95) WITHIN GROUP (ORDER BY COALESCE(duration_ms, 0)), 0)
FROM spans
WHERE tenant_id = $1
  AND provider = $2
  AND span_type = 'llm_call'
  AND status IN ('completed', 'error')
  AND start_time >= $3
  AND start_time < $4`

const providerRecentErrorsQuery = `
SELECT start_time, COALESCE(error, '')
FROM spans
WHERE tenant_id = $1
  AND provider = $2
  AND span_type = 'llm_call'
  AND status = 'error'
  AND start_time >= $3
  AND start_time < $4
ORDER BY start_time DESC
LIMIT $5`

// GetProviderCallStats implements store.ProviderCallStatsStore.
func (s *PGTracingStore) GetProviderCallStats(ctx context.Context, q store.ProviderCallStatsQuery) (*store.ProviderCallStats, error) {
	out := &store.ProviderCallStats{RecentErrors: []store.ProviderErrorSpan{}}
	if err := s.db.QueryRowContext(ctx, providerCallStatsQuery, q.TenantID, q.Provider, q.From, q.To).
		Scan(&out.Calls, &out.Errors, &out.LatencyP50MS, &out.LatencyP95MS); err != nil {
		return nil, err
	}
	if out.Errors == 0 || q.ErrorLimit <= 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, providerRecentErrorsQuery, q.TenantID, q.Provider, q.From, q.To, q.ErrorLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if err := scanProviderErrorSpans(rows, out); err != nil {
		return nil, err
	}
	return out, nil
}

func scanProviderErrorSpans(rows *sql.Rows, out *store.ProviderCallStats) error {
	for rows.Next() {
		var e store.ProviderErrorSpan
		if err := rows.Scan(&e.At, &e.Error); err != nil {
			return err
		}
		out.RecentErrors = append(out.RecentErrors, e)
	}
	return rows.Err()
}

var _ store.ProviderCallStatsStore = (*PGTracingStore)(nil)

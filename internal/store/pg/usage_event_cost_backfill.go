package pg

import (
	"context"
	"fmt"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (s *PGUsageEventStore) BackfillUsageEventCosts(ctx context.Context) (store.UsageEventCostBackfillStats, error) {
	var stats store.UsageEventCostBackfillStats
	// Cost source of truth: usage_events.cost_usd is the billing truth for
	// rollups, and spans are the originating measure. This backfill reconciles
	// every linked event against its span so a later span cost correction (e.g.
	// after a pricing sync) propagates to the event and downstream rollups.
	// Zero-cost events still get filled; diverging costs get overwritten.
	// Span cost itself is retained and never dropped.
	rows, err := s.db.QueryContext(ctx, `
WITH linked_costs AS (
	SELECT
		e.id,
		e.bucket_hour,
		s.total_cost
	FROM usage_events e
	JOIN spans s
	  ON s.id = e.span_id
	 AND s.trace_id = e.trace_id
	 AND s.tenant_id = e.tenant_id
	WHERE COALESCE(s.total_cost, 0) > 0
)
UPDATE usage_events e
SET cost_usd = linked_costs.total_cost
FROM linked_costs
WHERE e.id = linked_costs.id
  AND e.cost_usd IS DISTINCT FROM linked_costs.total_cost
RETURNING e.bucket_hour`)
	if err != nil {
		return stats, fmt.Errorf("backfill usage event costs: %w", err)
	}
	defer rows.Close()

	bucketSeen := make(map[time.Time]struct{})
	for rows.Next() {
		var bucket time.Time
		if err := rows.Scan(&bucket); err != nil {
			return stats, fmt.Errorf("scan usage event cost backfill bucket: %w", err)
		}
		stats.EventRowsUpdated++
		bucket = bucket.UTC().Truncate(time.Hour)
		if _, ok := bucketSeen[bucket]; !ok {
			bucketSeen[bucket] = struct{}{}
			stats.RollupBuckets = append(stats.RollupBuckets, bucket)
		}
	}
	if err := rows.Err(); err != nil {
		return stats, err
	}

	for _, bucket := range stats.RollupBuckets {
		if err := s.RefreshEventRollupHour(ctx, bucket); err != nil {
			return stats, fmt.Errorf("refresh usage event rollup %s: %w", bucket.Format(time.RFC3339), err)
		}
	}
	return stats, nil
}

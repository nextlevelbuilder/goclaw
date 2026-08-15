package http

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func liveUsageWindow(q store.SnapshotQuery, now time.Time) (time.Time, time.Time, bool) {
	now = now.UTC()
	from := now.Truncate(time.Hour)
	if q.From.After(from) {
		from = q.From.UTC()
	}
	to := now
	if !q.To.IsZero() && q.To.Before(to) {
		to = q.To.UTC()
	}
	if !to.After(from) {
		return time.Time{}, time.Time{}, false
	}
	return from, to, true
}

func liveUsageBucket(from time.Time, groupBy string) time.Time {
	from = from.UTC()
	if groupBy == "day" {
		return time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	}
	return from.Truncate(time.Hour)
}

func (h *UsageHandler) queryLiveTimeSeries(r *http.Request, from, to time.Time, q store.SnapshotQuery) (*store.SnapshotTimeSeries, error) {
	if h.db == nil {
		return nil, nil
	}

	point := store.SnapshotTimeSeries{BucketTime: liveUsageBucket(from, q.GroupBy)}
	if q.Provider == "" && q.Model == "" {
		if err := h.queryLiveTraceTotals(r, from, to, q, &point); err != nil {
			return nil, err
		}
	}
	if err := h.queryLiveSpanTotals(r, from, to, q, &point); err != nil {
		return nil, err
	}
	if !hasTimeSeriesUsage(point) {
		return nil, nil
	}
	return &point, nil
}

func (h *UsageHandler) queryLiveTraceTotals(r *http.Request, from, to time.Time, q store.SnapshotQuery, point *store.SnapshotTimeSeries) error {
	var query strings.Builder
	query.WriteString(`SELECT
		COUNT(*),
		COALESCE(SUM(CASE WHEN t.status = 'error' THEN 1 ELSE 0 END), 0),
		COUNT(DISTINCT t.user_id),
		COALESCE(SUM(t.tool_call_count), 0),
		CAST(COALESCE(AVG(t.duration_ms), 0) AS INTEGER)
	FROM traces t
	WHERE t.start_time >= $1 AND t.start_time < $2
	  AND t.parent_trace_id IS NULL`)

	args := []any{from, to}
	idx := 3
	appendLiveTraceFilters(&query, &args, &idx, r, q)

	return h.db.QueryRowContext(r.Context(), query.String(), args...).Scan(
		&point.RequestCount,
		&point.ErrorCount,
		&point.UniqueUsers,
		&point.ToolCallCount,
		&point.AvgDurationMS,
	)
}

func (h *UsageHandler) queryLiveSpanTotals(r *http.Request, from, to time.Time, q store.SnapshotQuery, point *store.SnapshotTimeSeries) error {
	var query strings.Builder
	query.WriteString(`SELECT
		COALESCE(SUM(COALESCE(s.input_tokens, 0)), 0),
		COALESCE(SUM(COALESCE(s.output_tokens, 0)), 0),
		COALESCE(SUM(COALESCE(s.total_cost, 0)), 0),
		COUNT(*) FILTER (WHERE s.span_type = 'llm_call'),
		COALESCE(SUM(CAST(COALESCE(NULLIF(s.metadata->>'cache_read_tokens', ''), '0') AS INTEGER)), 0),
		COALESCE(SUM(CAST(COALESCE(NULLIF(s.metadata->>'cache_creation_tokens', ''), '0') AS INTEGER)), 0),
		COALESCE(SUM(CAST(COALESCE(NULLIF(s.metadata->>'thinking_tokens', ''), '0') AS INTEGER)), 0)
	FROM traces t
	JOIN spans s ON s.trace_id = t.id AND s.span_type IN ('llm_call', 'tool_call')
	WHERE t.start_time >= $1 AND t.start_time < $2
	  AND t.parent_trace_id IS NULL`)

	args := []any{from, to}
	idx := 3
	appendLiveTraceFilters(&query, &args, &idx, r, q)
	appendLiveSpanFilters(&query, &args, &idx, q)

	return h.db.QueryRowContext(r.Context(), query.String(), args...).Scan(
		&point.InputTokens,
		&point.OutputTokens,
		&point.TotalCost,
		&point.LLMCallCount,
		&point.CacheReadTokens,
		&point.CacheCreateTokens,
		&point.ThinkingTokens,
	)
}

func appendLiveTraceFilters(query *strings.Builder, args *[]any, idx *int, r *http.Request, q store.SnapshotQuery) {
	if !store.IsCrossTenant(r.Context()) {
		if tenantID := store.TenantIDFromContext(r.Context()); tenantID != uuid.Nil {
			fmt.Fprintf(query, " AND t.tenant_id = $%d", *idx)
			*args = append(*args, tenantID)
			*idx += 1
		}
	}
	if q.AgentID != nil {
		fmt.Fprintf(query, " AND t.agent_id = $%d", *idx)
		*args = append(*args, *q.AgentID)
		*idx += 1
	}
	if q.Channel != "" {
		fmt.Fprintf(query, " AND t.channel = $%d", *idx)
		*args = append(*args, q.Channel)
		*idx += 1
	}
}

func appendLiveSpanFilters(query *strings.Builder, args *[]any, idx *int, q store.SnapshotQuery) {
	if q.Provider != "" {
		fmt.Fprintf(query, " AND s.provider = $%d", *idx)
		*args = append(*args, q.Provider)
		*idx += 1
	}
	if q.Model != "" {
		fmt.Fprintf(query, " AND s.model = $%d", *idx)
		*args = append(*args, q.Model)
		*idx += 1
	}
}

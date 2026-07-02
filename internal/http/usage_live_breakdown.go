package http

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

func (h *UsageHandler) queryLiveBreakdown(r *http.Request, from, to time.Time, q store.SnapshotQuery) ([]store.SnapshotBreakdown, error) {
	if h.db == nil {
		return nil, nil
	}
	groupBy := q.GroupBy
	if groupBy == "" {
		groupBy = "provider"
	}

	switch groupBy {
	case "channel", "agent":
		spanRows, err := h.queryLiveSpanBreakdown(r, from, to, q, groupBy)
		if err != nil {
			return nil, err
		}
		if q.Provider != "" || q.Model != "" {
			return spanRows, nil
		}
		traceRows, err := h.queryLiveTraceBreakdown(r, from, to, q, groupBy)
		if err != nil {
			return nil, err
		}
		return mergeSnapshotBreakdowns(traceRows, spanRows, groupBy), nil
	default:
		return h.queryLiveSpanBreakdown(r, from, to, q, groupBy)
	}
}

func (h *UsageHandler) queryLiveSpanBreakdown(r *http.Request, from, to time.Time, q store.SnapshotQuery, groupBy string) ([]store.SnapshotBreakdown, error) {
	groupExpr := "COALESCE(s.provider, '')"
	extraFilter := " AND COALESCE(s.provider, '') != '' AND COALESCE(s.model, '') != ''"
	switch groupBy {
	case "model":
		groupExpr = "COALESCE(s.model, '')"
	case "provider_model":
		groupExpr = "COALESCE(s.provider, '') || '/' || COALESCE(s.model, '')"
	case "channel":
		groupExpr = "COALESCE(t.channel, '')"
		extraFilter = " AND COALESCE(t.channel, '') != ''"
	case "agent":
		groupExpr = "COALESCE(CAST(t.agent_id AS TEXT), '')"
		extraFilter = ""
	}

	var query strings.Builder
	fmt.Fprintf(&query, `SELECT
		%s AS key,
		COALESCE(SUM(COALESCE(s.input_tokens, 0)), 0),
		COALESCE(SUM(COALESCE(s.output_tokens, 0)), 0),
		COALESCE(SUM(CAST(COALESCE(NULLIF(s.metadata->>'cache_read_tokens', ''), '0') AS INTEGER)), 0),
		COALESCE(SUM(CAST(COALESCE(NULLIF(s.metadata->>'cache_creation_tokens', ''), '0') AS INTEGER)), 0),
		COALESCE(SUM(COALESCE(s.total_cost, 0)), 0),
		0,
		COUNT(*) FILTER (WHERE s.span_type = 'llm_call'),
		0,
		0,
		0
	FROM traces t
	JOIN spans s ON s.trace_id = t.id AND s.span_type IN ('llm_call', 'tool_call')
	WHERE t.start_time >= $1 AND t.start_time < $2
	  AND t.parent_trace_id IS NULL`, groupExpr)
	query.WriteString(extraFilter)

	args := []any{from, to}
	idx := 3
	appendLiveTraceFilters(&query, &args, &idx, r, q)
	appendLiveSpanFilters(&query, &args, &idx, q)
	fmt.Fprintf(&query, " GROUP BY %s", groupExpr)

	return scanLiveBreakdownRows(r, h, query.String(), args...)
}

func (h *UsageHandler) queryLiveTraceBreakdown(r *http.Request, from, to time.Time, q store.SnapshotQuery, groupBy string) ([]store.SnapshotBreakdown, error) {
	groupExpr := "COALESCE(t.channel, '')"
	extraFilter := " AND COALESCE(t.channel, '') != ''"
	if groupBy == "agent" {
		groupExpr = "COALESCE(CAST(t.agent_id AS TEXT), '')"
		extraFilter = ""
	}

	var query strings.Builder
	fmt.Fprintf(&query, `SELECT
		%s AS key,
		0,
		0,
		0,
		0,
		0,
		COUNT(*),
		0,
		COALESCE(SUM(t.tool_call_count), 0),
		COALESCE(SUM(CASE WHEN t.status = 'error' THEN 1 ELSE 0 END), 0),
		CAST(COALESCE(AVG(t.duration_ms), 0) AS INTEGER)
	FROM traces t
	WHERE t.start_time >= $1 AND t.start_time < $2
	  AND t.parent_trace_id IS NULL`, groupExpr)
	query.WriteString(extraFilter)

	args := []any{from, to}
	idx := 3
	appendLiveTraceFilters(&query, &args, &idx, r, q)
	fmt.Fprintf(&query, " GROUP BY %s", groupExpr)

	return scanLiveBreakdownRows(r, h, query.String(), args...)
}

func scanLiveBreakdownRows(r *http.Request, h *UsageHandler, query string, args ...any) ([]store.SnapshotBreakdown, error) {
	rows, err := h.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []store.SnapshotBreakdown
	for rows.Next() {
		var b store.SnapshotBreakdown
		if err := rows.Scan(
			&b.Key,
			&b.InputTokens,
			&b.OutputTokens,
			&b.CacheReadTokens,
			&b.CacheCreateTokens,
			&b.TotalCost,
			&b.RequestCount,
			&b.LLMCallCount,
			&b.ToolCallCount,
			&b.ErrorCount,
			&b.AvgDurationMS,
		); err != nil {
			return nil, err
		}
		result = append(result, b)
	}
	return result, rows.Err()
}

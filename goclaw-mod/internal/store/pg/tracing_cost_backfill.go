package pg

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const pricedLLMSpansCTE = `
WITH priced_spans AS (
	SELECT
		s.id AS span_id,
		s.trace_id,
		t.start_time AS trace_start_time,
		calc.total_cost
	FROM spans s
	JOIN traces t ON t.id = s.trace_id AND t.tenant_id = s.tenant_id
	LEFT JOIN llm_providers lp ON lp.tenant_id = s.tenant_id AND lp.name = s.provider
	CROSS JOIN LATERAL (
		SELECT
			btrim(COALESCE(s.model, '')) AS model_id,
			replace(lower(btrim(COALESCE(lp.name, s.provider, ''))), ' ', '-') AS provider_name,
			COALESCE(lp.provider_type, '') AS provider_type
	) base
	CROSS JOIN LATERAL (
		SELECT ARRAY_REMOVE(ARRAY[
			NULLIF(base.model_id, '')::text,
			CASE
				WHEN position('/' IN base.model_id) > 0 THEN NULL
				WHEN base.provider_type = 'anthropic_native' THEN 'anthropic/' || base.model_id
				WHEN base.provider_type IN ('gemini_native', 'vertex') THEN 'google/' || base.model_id
				WHEN base.provider_type = 'openai_compat' AND base.provider_name IN ('openai', 'azure', 'azure-openai', 'azure_openai') THEN 'openai/' || base.model_id
				WHEN base.provider_type = 'openai_compat' AND base.provider_name = 'anthropic' THEN 'anthropic/' || base.model_id
				WHEN base.provider_type = 'openai_compat' AND base.provider_name IN ('gemini', 'google', 'vertex') THEN 'google/' || base.model_id
				WHEN base.provider_type = 'groq' THEN 'groq/' || base.model_id
				WHEN base.provider_type = 'deepseek' THEN 'deepseek/' || base.model_id
				WHEN base.provider_type = 'mistral' THEN 'mistralai/' || base.model_id
				WHEN base.provider_type = 'xai' THEN 'x-ai/' || base.model_id
				WHEN base.provider_type = 'minimax_native' THEN 'minimax/' || base.model_id
				WHEN base.provider_type = 'cohere' THEN 'cohere/' || base.model_id
				WHEN base.provider_type = 'perplexity' THEN 'perplexity/' || base.model_id
				WHEN base.provider_type IN ('dashscope', 'bailian') THEN 'qwen/' || base.model_id
				ELSE NULL
			END,
			CASE WHEN position('/' IN base.model_id) = 0 AND lower(base.model_id) LIKE 'gpt-%' THEN 'openai/' || base.model_id END,
			CASE WHEN position('/' IN base.model_id) = 0 AND (lower(base.model_id) LIKE 'o1%' OR lower(base.model_id) LIKE 'o3%' OR lower(base.model_id) LIKE 'o4%' OR lower(base.model_id) LIKE 'o5%') THEN 'openai/' || base.model_id END,
			CASE WHEN position('/' IN base.model_id) = 0 AND lower(base.model_id) LIKE 'qwen%' THEN 'qwen/' || base.model_id END,
			CASE WHEN position('/' IN base.model_id) = 0 AND lower(base.model_id) LIKE 'claude-%' THEN 'anthropic/' || base.model_id END,
			CASE WHEN position('/' IN base.model_id) = 0 AND lower(base.model_id) LIKE 'gemini-%' THEN 'google/' || base.model_id END
		], NULL)::text[] AS model_ids
	) candidates
	CROSS JOIN LATERAL (
		SELECT
			p.input_price,
			p.output_price,
			p.cache_read_price,
			p.cache_write_price,
			p.reasoning_price
		FROM (
			SELECT
				0 AS source_priority,
				COALESCE(array_position(candidates.model_ids, o.model_id), 2147483647) AS candidate_order,
				o.input_price,
				o.output_price,
				o.cache_read_price,
				o.cache_write_price,
				o.reasoning_price
			FROM usage_pricing_overrides o
			WHERE lp.id IS NOT NULL
			  AND o.tenant_id = s.tenant_id
			  AND o.provider_id = lp.id
			  AND o.enabled = true
			  AND o.model_id = ANY(candidates.model_ids)
			UNION ALL
			SELECT
				1 AS source_priority,
				LEAST(
					COALESCE(array_position(candidates.model_ids, c.model_id), 2147483647),
					COALESCE(array_position(candidates.model_ids, c.canonical_model_id), 2147483647)
				) AS candidate_order,
				c.input_price,
				c.output_price,
				c.cache_read_price,
				c.cache_write_price,
				c.reasoning_price
			FROM usage_pricing_catalog c
			WHERE c.model_id = ANY(candidates.model_ids)
			   OR c.canonical_model_id = ANY(candidates.model_ids)
		) p
		ORDER BY p.source_priority, p.candidate_order
		LIMIT 1
	) price
	CROSS JOIN LATERAL (
		SELECT
			GREATEST(
				COALESCE(s.input_tokens, 0)::numeric -
				CASE
					WHEN lower(COALESCE(s.metadata->>'prompt_tokens_include_cached_segments', '')) = 'true'
					  OR (
						NOT (s.metadata ? 'prompt_tokens_include_cached_segments')
						AND base.provider_type IN ('openai_compat', 'openrouter', 'dashscope', 'bailian', 'chatgpt_oauth')
					  )
					THEN
						(CASE WHEN s.metadata ? 'cache_read_tokens' AND (s.metadata->>'cache_read_tokens') ~ '^[0-9]+$'
							THEN (s.metadata->>'cache_read_tokens')::numeric ELSE 0 END) +
						(CASE WHEN s.metadata ? 'cache_creation_tokens' AND (s.metadata->>'cache_creation_tokens') ~ '^[0-9]+$'
							THEN (s.metadata->>'cache_creation_tokens')::numeric ELSE 0 END)
					ELSE 0
				END,
				0
			) AS input_tokens,
			COALESCE(s.output_tokens, 0)::numeric AS output_tokens,
			CASE WHEN s.metadata ? 'cache_read_tokens' AND (s.metadata->>'cache_read_tokens') ~ '^[0-9]+$'
				THEN (s.metadata->>'cache_read_tokens')::numeric ELSE 0 END AS cache_read_tokens,
			CASE WHEN s.metadata ? 'cache_creation_tokens' AND (s.metadata->>'cache_creation_tokens') ~ '^[0-9]+$'
				THEN (s.metadata->>'cache_creation_tokens')::numeric ELSE 0 END AS cache_write_tokens,
			CASE WHEN s.metadata ? 'thinking_tokens' AND (s.metadata->>'thinking_tokens') ~ '^[0-9]+$'
				THEN (s.metadata->>'thinking_tokens')::numeric ELSE 0 END AS reasoning_tokens
	) usage
	CROSS JOIN LATERAL (
		SELECT (
			CASE WHEN usage.input_tokens > 0 THEN CEIL(usage.input_tokens * price.input_price * 1000000) / 1000000 ELSE 0 END +
			CASE
				WHEN usage.output_tokens > 0 AND usage.reasoning_tokens > 0 AND price.reasoning_price IS NOT NULL THEN
					CEIL(GREATEST(usage.output_tokens - usage.reasoning_tokens, 0) * price.output_price * 1000000) / 1000000 +
					CEIL(usage.reasoning_tokens * price.reasoning_price * 1000000) / 1000000
				WHEN usage.output_tokens > 0 THEN CEIL(usage.output_tokens * price.output_price * 1000000) / 1000000
				ELSE 0
			END +
			CASE WHEN usage.cache_read_tokens > 0 THEN CEIL(usage.cache_read_tokens * price.cache_read_price * 1000000) / 1000000 ELSE 0 END +
			CASE WHEN usage.cache_write_tokens > 0 THEN CEIL(usage.cache_write_tokens * price.cache_write_price * 1000000) / 1000000 ELSE 0 END
		)::numeric(12,8) AS total_cost
	) calc
	WHERE s.span_type IN ('llm_call', 'tool_call')
	  AND base.model_id <> ''
	  AND COALESCE(s.total_cost, 0) = 0
	  AND (usage.input_tokens = 0 OR price.input_price IS NOT NULL)
	  AND (usage.output_tokens = 0 OR price.output_price IS NOT NULL)
	  AND (usage.cache_read_tokens = 0 OR price.cache_read_price IS NOT NULL)
	  AND (usage.cache_write_tokens = 0 OR price.cache_write_price IS NOT NULL)
	  AND calc.total_cost > 0
)`

func (s *PGTracingStore) BackfillLLMCosts(ctx context.Context) (store.TraceCostBackfillStats, error) {
	var stats store.TraceCostBackfillStats
	traceIDs, buckets, err := s.listPricedLLMBackfillTargets(ctx)
	if err != nil {
		return stats, err
	}
	if len(traceIDs) == 0 {
		return stats, nil
	}
	stats.SnapshotBuckets = buckets

	res, err := s.db.ExecContext(ctx, pricedLLMSpansCTE+`
UPDATE spans s
SET total_cost = priced_spans.total_cost
FROM priced_spans
WHERE s.id = priced_spans.span_id
  AND s.total_cost IS DISTINCT FROM priced_spans.total_cost`)
	if err != nil {
		return stats, fmt.Errorf("backfill span costs: %w", err)
	}
	stats.SpanRowsUpdated, _ = res.RowsAffected()

	stats.TraceRowsUpdated, err = s.refreshTraceCostAggregates(ctx, traceIDs)
	if err != nil {
		return stats, err
	}
	return stats, nil
}

func (s *PGTracingStore) listPricedLLMBackfillTargets(ctx context.Context) ([]uuid.UUID, []time.Time, error) {
	rows, err := s.db.QueryContext(ctx, pricedLLMSpansCTE+`
SELECT DISTINCT trace_id, date_trunc('hour', trace_start_time) AS bucket_hour
FROM priced_spans`)
	if err != nil {
		return nil, nil, fmt.Errorf("list priced span targets: %w", err)
	}
	defer rows.Close()

	traceSeen := make(map[uuid.UUID]struct{})
	bucketSeen := make(map[time.Time]struct{})
	var traceIDs []uuid.UUID
	var buckets []time.Time
	for rows.Next() {
		var traceID uuid.UUID
		var bucket time.Time
		if err := rows.Scan(&traceID, &bucket); err != nil {
			return nil, nil, err
		}
		if _, ok := traceSeen[traceID]; !ok {
			traceSeen[traceID] = struct{}{}
			traceIDs = append(traceIDs, traceID)
		}
		bucket = bucket.UTC().Truncate(time.Hour)
		if _, ok := bucketSeen[bucket]; !ok {
			bucketSeen[bucket] = struct{}{}
			buckets = append(buckets, bucket)
		}
	}
	return traceIDs, buckets, rows.Err()
}

func (s *PGTracingStore) refreshTraceCostAggregates(ctx context.Context, traceIDs []uuid.UUID) (int64, error) {
	if len(traceIDs) == 0 {
		return 0, nil
	}
	ids := make([]string, len(traceIDs))
	for i, id := range traceIDs {
		ids[i] = id.String()
	}
	res, err := s.db.ExecContext(ctx, `
WITH agg AS (
	SELECT
		trace_id,
		COUNT(*) AS span_count,
		COUNT(*) FILTER (WHERE span_type = 'llm_call') AS llm_call_count,
		COUNT(*) FILTER (WHERE span_type = 'tool_call') AS tool_call_count,
		COALESCE(SUM(input_tokens) FILTER (WHERE span_type IN ('llm_call', 'tool_call') AND input_tokens IS NOT NULL), 0) AS total_input_tokens,
		COALESCE(SUM(output_tokens) FILTER (WHERE span_type IN ('llm_call', 'tool_call') AND output_tokens IS NOT NULL), 0) AS total_output_tokens,
		COALESCE(SUM(total_cost) FILTER (WHERE total_cost IS NOT NULL), 0) AS total_cost,
		COALESCE(SUM(CASE WHEN metadata ? 'cache_read_tokens' AND (metadata->>'cache_read_tokens') ~ '^[0-9]+$'
			THEN (metadata->>'cache_read_tokens')::int ELSE 0 END), 0) AS cache_read_tokens,
		COALESCE(SUM(CASE WHEN metadata ? 'cache_creation_tokens' AND (metadata->>'cache_creation_tokens') ~ '^[0-9]+$'
			THEN (metadata->>'cache_creation_tokens')::int ELSE 0 END), 0) AS cache_creation_tokens
	FROM spans
	WHERE trace_id = ANY($1::uuid[])
	GROUP BY trace_id
)
UPDATE traces t
SET
	span_count = agg.span_count,
	llm_call_count = agg.llm_call_count,
	tool_call_count = agg.tool_call_count,
	total_input_tokens = agg.total_input_tokens,
	total_output_tokens = agg.total_output_tokens,
	total_cost = agg.total_cost,
	metadata = jsonb_build_object(
		'total_cache_read_tokens', agg.cache_read_tokens,
		'total_cache_creation_tokens', agg.cache_creation_tokens
	)
FROM agg
WHERE t.id = agg.trace_id`, pq.Array(ids))
	if err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, fmt.Errorf("refresh trace cost aggregates: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (s *PGTracingStore) ReconcileTraceUsageAggregates(ctx context.Context) (store.TraceUsageAggregateStats, error) {
	var stats store.TraceUsageAggregateStats
	res, err := s.db.ExecContext(ctx, `
WITH agg AS (
	SELECT
		trace_id,
		COUNT(*) AS span_count,
		COUNT(*) FILTER (WHERE span_type = 'llm_call') AS llm_call_count,
		COUNT(*) FILTER (WHERE span_type = 'tool_call') AS tool_call_count,
		COALESCE(SUM(input_tokens) FILTER (WHERE span_type IN ('llm_call', 'tool_call') AND input_tokens IS NOT NULL), 0) AS total_input_tokens,
		COALESCE(SUM(output_tokens) FILTER (WHERE span_type IN ('llm_call', 'tool_call') AND output_tokens IS NOT NULL), 0) AS total_output_tokens,
		COALESCE(SUM(total_cost) FILTER (WHERE total_cost IS NOT NULL), 0) AS total_cost,
		COALESCE(SUM(CASE WHEN metadata ? 'cache_read_tokens' AND (metadata->>'cache_read_tokens') ~ '^[0-9]+$'
			THEN (metadata->>'cache_read_tokens')::int ELSE 0 END), 0) AS cache_read_tokens,
		COALESCE(SUM(CASE WHEN metadata ? 'cache_creation_tokens' AND (metadata->>'cache_creation_tokens') ~ '^[0-9]+$'
			THEN (metadata->>'cache_creation_tokens')::int ELSE 0 END), 0) AS cache_creation_tokens
	FROM spans
	GROUP BY trace_id
)
UPDATE traces t
SET
	span_count = agg.span_count,
	llm_call_count = agg.llm_call_count,
	tool_call_count = agg.tool_call_count,
	total_input_tokens = agg.total_input_tokens,
	total_output_tokens = agg.total_output_tokens,
	total_cost = agg.total_cost,
	metadata = jsonb_build_object(
		'total_cache_read_tokens', agg.cache_read_tokens,
		'total_cache_creation_tokens', agg.cache_creation_tokens
	)
FROM agg
WHERE t.id = agg.trace_id
  AND (
	t.span_count IS DISTINCT FROM agg.span_count OR
	t.llm_call_count IS DISTINCT FROM agg.llm_call_count OR
	t.tool_call_count IS DISTINCT FROM agg.tool_call_count OR
	t.total_input_tokens IS DISTINCT FROM agg.total_input_tokens OR
	t.total_output_tokens IS DISTINCT FROM agg.total_output_tokens OR
	t.total_cost IS DISTINCT FROM agg.total_cost
  )`)
	if err != nil {
		return stats, fmt.Errorf("reconcile trace usage aggregates: %w", err)
	}
	stats.TraceRowsUpdated, _ = res.RowsAffected()
	return stats, nil
}

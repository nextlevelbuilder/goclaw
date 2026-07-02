package pg

import (
	"context"
	"fmt"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const pricedUsageSnapshotsCTE = `
WITH priced_snapshots AS (
	SELECT
		us.id AS snapshot_id,
		calc.total_cost
	FROM usage_snapshots us
	LEFT JOIN llm_providers lp ON lp.tenant_id = us.tenant_id AND lp.name = us.provider
	CROSS JOIN LATERAL (
		SELECT
			btrim(COALESCE(us.model, '')) AS model_id,
			replace(lower(btrim(COALESCE(lp.name, us.provider, ''))), ' ', '-') AS provider_name,
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
			  AND o.tenant_id = us.tenant_id
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
				COALESCE(us.input_tokens, 0)::numeric -
				CASE
					WHEN base.provider_type IN ('openai_compat', 'openrouter', 'dashscope', 'bailian', 'chatgpt_oauth')
					THEN COALESCE(us.cache_read_tokens, 0)::numeric + COALESCE(us.cache_create_tokens, 0)::numeric
					ELSE 0
				END,
				0
			) AS input_tokens,
			COALESCE(us.output_tokens, 0)::numeric AS output_tokens,
			COALESCE(us.cache_read_tokens, 0)::numeric AS cache_read_tokens,
			COALESCE(us.cache_create_tokens, 0)::numeric AS cache_write_tokens,
			COALESCE(us.thinking_tokens, 0)::numeric AS reasoning_tokens
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
	WHERE COALESCE(us.provider, '') <> ''
	  AND base.model_id <> ''
	  AND COALESCE(us.total_cost, 0) = 0
	  AND COALESCE(us.input_tokens, 0) + COALESCE(us.output_tokens, 0) + COALESCE(us.cache_read_tokens, 0) + COALESCE(us.cache_create_tokens, 0) + COALESCE(us.thinking_tokens, 0) > 0
	  AND (usage.input_tokens = 0 OR price.input_price IS NOT NULL)
	  AND (usage.output_tokens = 0 OR price.output_price IS NOT NULL)
	  AND (usage.cache_read_tokens = 0 OR price.cache_read_price IS NOT NULL)
	  AND (usage.cache_write_tokens = 0 OR price.cache_write_price IS NOT NULL)
	  AND calc.total_cost > 0
)`

func (s *PGSnapshotStore) BackfillSnapshotCosts(ctx context.Context) (store.SnapshotCostBackfillStats, error) {
	var stats store.SnapshotCostBackfillStats
	res, err := s.db.ExecContext(ctx, pricedUsageSnapshotsCTE+`
UPDATE usage_snapshots us
SET total_cost = priced_snapshots.total_cost
FROM priced_snapshots
WHERE us.id = priced_snapshots.snapshot_id
  AND us.total_cost IS DISTINCT FROM priced_snapshots.total_cost`)
	if err != nil {
		return stats, fmt.Errorf("backfill snapshot costs: %w", err)
	}
	stats.SnapshotRowsUpdated, _ = res.RowsAffected()
	return stats, nil
}

package pg

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

const listCodexPoolSpansQuery = `
SELECT
	sp.id,
	sp.trace_id,
	sp.start_time,
	COALESCE(sp.duration_ms, 0),
	sp.status,
	COALESCE(sp.provider, ''),
	COALESCE(sp.model, ''),
	COALESCE(sp.metadata, '{}'::jsonb)
FROM spans sp
JOIN traces t ON t.id = sp.trace_id
WHERE t.agent_id = $1
  AND t.tenant_id = $2
  AND t.parent_trace_id IS NULL
  AND sp.tenant_id = $2
  AND sp.span_type = 'llm_call'
  AND (
	sp.provider = ANY($3)
	OR COALESCE(sp.metadata->'chatgpt_oauth_routing'->>'selected_provider', '') = ANY($3)
	OR COALESCE(sp.metadata->'chatgpt_oauth_routing'->>'serving_provider', '') = ANY($3)
  )
ORDER BY sp.start_time DESC
LIMIT $4`

// ListCodexPoolSpans returns recent LLM call spans for agents using Codex OAuth pool providers.
func (s *PGTracingStore) ListCodexPoolSpans(ctx context.Context, agentID, tenantID uuid.UUID, poolProviders []string, limit int) ([]store.CodexPoolSpan, error) {
	rows, err := s.db.QueryContext(ctx, listCodexPoolSpansQuery, agentID, tenantID, pq.Array(poolProviders), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	spans := make([]store.CodexPoolSpan, 0, limit)
	for rows.Next() {
		var item store.CodexPoolSpan
		var metadata json.RawMessage
		if err := rows.Scan(
			&item.SpanID,
			&item.TraceID,
			&item.StartedAt,
			&item.DurationMS,
			&item.Status,
			&item.Provider,
			&item.Model,
			&metadata,
		); err != nil {
			return nil, err
		}
		item.Metadata = metadata
		spans = append(spans, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return spans, nil
}

const listCodexPoolSpansByProvidersQuery = `
SELECT
	sp.id,
	sp.trace_id,
	sp.start_time,
	COALESCE(sp.duration_ms, 0),
	sp.status,
	COALESCE(sp.provider, ''),
	COALESCE(sp.model, ''),
	COALESCE(sp.metadata, '{}'::jsonb),
	t.agent_id
FROM spans sp
JOIN traces t ON t.id = sp.trace_id
WHERE t.tenant_id = $1
  AND t.parent_trace_id IS NULL
  AND sp.tenant_id = $1
  AND sp.span_type = 'llm_call'
  AND sp.start_time > NOW() - INTERVAL '7 days'
  AND (
	sp.provider = ANY($2)
	OR COALESCE(sp.metadata->'chatgpt_oauth_routing'->>'selected_provider', '') = ANY($2)
	OR COALESCE(sp.metadata->'chatgpt_oauth_routing'->>'serving_provider', '') = ANY($2)
  )
ORDER BY sp.start_time DESC
LIMIT $3`

// ListCodexPoolSpansByProviders returns recent LLM call spans across all agents for the given pool providers.
func (s *PGTracingStore) ListCodexPoolSpansByProviders(ctx context.Context, tenantID uuid.UUID, poolProviders []string, limit int) ([]store.CodexPoolProviderSpan, error) {
	rows, err := s.db.QueryContext(ctx, listCodexPoolSpansByProvidersQuery, tenantID, pq.Array(poolProviders), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	spans := make([]store.CodexPoolProviderSpan, 0, limit)
	for rows.Next() {
		var item store.CodexPoolProviderSpan
		var metadata json.RawMessage
		if err := rows.Scan(
			&item.SpanID,
			&item.TraceID,
			&item.StartedAt,
			&item.DurationMS,
			&item.Status,
			&item.Provider,
			&item.Model,
			&metadata,
			&item.AgentID,
		); err != nil {
			return nil, err
		}
		item.Metadata = metadata
		spans = append(spans, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return spans, nil
}

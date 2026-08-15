-- GIN indexes for JSONB columns that are actively queried with -> / ->> operators.
-- Audit found 0 existing GIN indexes on 30+ JSONB columns.

-- spans.metadata: used for cache token aggregation (tracing.go, snapshot_worker.go)
-- and chatgpt_oauth_routing evidence queries (agents_codex_pool_activity.go).
-- Partial index on span_type = 'llm_call' to reduce INSERT overhead on high-volume table.
CREATE INDEX IF NOT EXISTS idx_spans_metadata_gin
  ON spans USING GIN (metadata)
  WHERE span_type = 'llm_call';

-- sessions.metadata: used for chat_title filtering in pending message lookups
-- (pending_message_store.go) and heartbeat queries (heartbeat.go).
CREATE INDEX IF NOT EXISTS idx_sessions_metadata_gin
  ON sessions USING GIN (metadata);

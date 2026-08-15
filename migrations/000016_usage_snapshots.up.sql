-- ============================================================
-- Part 1: New indexes on EXISTING tables (optimize aggregation)
-- ============================================================

-- Traces: snapshot worker scans by start_time for root traces only
-- Replaces Seq Scan (2.5ms→0.1ms at current scale, critical at 100K+ rows)
CREATE INDEX IF NOT EXISTS idx_traces_start_root ON traces (start_time DESC)
    WHERE parent_trace_id IS NULL;

-- Spans: snapshot worker joins on trace_id filtering by span_type
-- Current idx_spans_trace is (trace_id, start_time) — start_time useless here
-- This index lets PG filter span_type IN the index, avoiding wide-row fetches
CREATE INDEX IF NOT EXISTS idx_spans_trace_type ON spans (trace_id, span_type);

-- ============================================================
-- Part 2: usage_snapshots table
-- ============================================================

CREATE TABLE IF NOT EXISTS usage_snapshots (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    bucket_hour TIMESTAMPTZ NOT NULL,
    agent_id    UUID,
    provider    VARCHAR(50) NOT NULL DEFAULT '',
    model       VARCHAR(200) NOT NULL DEFAULT '',
    channel     VARCHAR(50) NOT NULL DEFAULT '',

    -- Token metrics
    input_tokens        BIGINT NOT NULL DEFAULT 0,
    output_tokens       BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens   BIGINT NOT NULL DEFAULT 0,
    cache_create_tokens BIGINT NOT NULL DEFAULT 0,
    thinking_tokens     BIGINT NOT NULL DEFAULT 0,

    -- Cost
    total_cost          NUMERIC(12,6) NOT NULL DEFAULT 0,

    -- Counts
    request_count       INTEGER NOT NULL DEFAULT 0,
    llm_call_count      INTEGER NOT NULL DEFAULT 0,
    tool_call_count     INTEGER NOT NULL DEFAULT 0,
    error_count         INTEGER NOT NULL DEFAULT 0,
    unique_users        INTEGER NOT NULL DEFAULT 0,

    -- Duration
    avg_duration_ms     INTEGER NOT NULL DEFAULT 0,

    -- Memory & Knowledge Graph (point-in-time counts)
    memory_docs         INTEGER NOT NULL DEFAULT 0,
    memory_chunks       INTEGER NOT NULL DEFAULT 0,
    kg_entities         INTEGER NOT NULL DEFAULT 0,
    kg_relations        INTEGER NOT NULL DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================
-- Part 3: usage_snapshots indexes
-- ============================================================

-- Time-series queries: GROUP BY bucket_hour ORDER BY bucket_hour
CREATE INDEX IF NOT EXISTS idx_usage_snapshots_bucket ON usage_snapshots (bucket_hour DESC);

-- Agent-scoped time-series: WHERE agent_id = $1 ORDER BY bucket_hour
CREATE INDEX IF NOT EXISTS idx_usage_snapshots_agent_bucket ON usage_snapshots (agent_id, bucket_hour DESC);

-- Cross-filter: WHERE provider = $1 AND bucket_hour BETWEEN ...
CREATE INDEX IF NOT EXISTS idx_usage_snapshots_provider_bucket ON usage_snapshots (provider, bucket_hour DESC)
    WHERE provider != '';

-- Cross-filter: WHERE channel = $1 AND bucket_hour BETWEEN ...
CREATE INDEX IF NOT EXISTS idx_usage_snapshots_channel_bucket ON usage_snapshots (channel, bucket_hour DESC)
    WHERE channel != '';

-- Upsert dedup: ON CONFLICT — prevents duplicate snapshot rows
CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_snapshots_unique ON usage_snapshots (
    bucket_hour,
    COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'),
    provider, model, channel
);

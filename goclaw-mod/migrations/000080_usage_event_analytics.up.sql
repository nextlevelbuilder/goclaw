CREATE TABLE IF NOT EXISTS usage_events (
    id            UUID PRIMARY KEY,
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    event_time    TIMESTAMPTZ NOT NULL,
    bucket_hour   TIMESTAMPTZ NOT NULL,
    event_type    TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_name TEXT NOT NULL,
    resource_id   TEXT NOT NULL DEFAULT '',
    source        TEXT NOT NULL DEFAULT '',
    agent_id      UUID REFERENCES agents(id) ON DELETE SET NULL,
    team_id       UUID,
    trace_id      UUID REFERENCES traces(id) ON DELETE SET NULL,
    span_id       UUID REFERENCES spans(id) ON DELETE SET NULL,
    run_id        TEXT NOT NULL DEFAULT '',
    session_key   TEXT NOT NULL DEFAULT '',
    channel       TEXT NOT NULL DEFAULT '',
    provider      TEXT NOT NULL DEFAULT '',
    model         TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT '',
    input_tokens  BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    total_tokens  BIGINT NOT NULL DEFAULT 0,
    cost_usd      DOUBLE PRECISION NOT NULL DEFAULT 0,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    call_count    INTEGER NOT NULL DEFAULT 1,
    error_count   INTEGER NOT NULL DEFAULT 0,
    metadata      JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_usage_events_tenant_time
    ON usage_events(tenant_id, event_time DESC);
CREATE INDEX IF NOT EXISTS idx_usage_events_tenant_resource_time
    ON usage_events(tenant_id, resource_type, resource_name, event_time DESC);
CREATE INDEX IF NOT EXISTS idx_usage_events_tenant_type_time
    ON usage_events(tenant_id, event_type, event_time DESC);
CREATE INDEX IF NOT EXISTS idx_usage_events_tenant_agent_time
    ON usage_events(tenant_id, agent_id, event_time DESC);
CREATE INDEX IF NOT EXISTS idx_usage_events_tenant_channel_time
    ON usage_events(tenant_id, channel, event_time DESC)
    WHERE channel != '';
CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_events_trace_span_type_source
    ON usage_events(trace_id, span_id, event_type, source)
    WHERE trace_id IS NOT NULL AND span_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS usage_event_rollups (
    id            UUID PRIMARY KEY,
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    bucket_hour   TIMESTAMPTZ NOT NULL,
    event_type    TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_name TEXT NOT NULL,
    source        TEXT NOT NULL DEFAULT '',
    agent_id      UUID REFERENCES agents(id) ON DELETE SET NULL,
    channel       TEXT NOT NULL DEFAULT '',
    provider      TEXT NOT NULL DEFAULT '',
    model         TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL DEFAULT '',
    input_tokens  BIGINT NOT NULL DEFAULT 0,
    output_tokens BIGINT NOT NULL DEFAULT 0,
    total_tokens  BIGINT NOT NULL DEFAULT 0,
    cost_usd      DOUBLE PRECISION NOT NULL DEFAULT 0,
    duration_ms   INTEGER NOT NULL DEFAULT 0,
    call_count    INTEGER NOT NULL DEFAULT 0,
    error_count   INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_event_rollups_unique
    ON usage_event_rollups (
        tenant_id,
        bucket_hour,
        event_type,
        resource_type,
        resource_name,
        source,
        COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'::uuid),
        channel,
        provider,
        model,
        status
    );
CREATE INDEX IF NOT EXISTS idx_usage_event_rollups_tenant_hour
    ON usage_event_rollups(tenant_id, bucket_hour DESC);
CREATE INDEX IF NOT EXISTS idx_usage_event_rollups_resource_hour
    ON usage_event_rollups(tenant_id, resource_type, resource_name, bucket_hour DESC);

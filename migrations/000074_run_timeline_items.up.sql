CREATE TABLE run_timeline_items (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    run_id       TEXT NOT NULL,
    session_key  VARCHAR(500) NOT NULL,
    agent_id     UUID REFERENCES agents(id) ON DELETE SET NULL,
    user_id      VARCHAR(255),
    channel      VARCHAR(50),
    chat_id      VARCHAR(255),
    seq          INT NOT NULL,
    item_type    VARCHAR(40) NOT NULL,
    status       VARCHAR(40),
    title        TEXT,
    preview      TEXT,
    content      TEXT NOT NULL DEFAULT '',
    tool_name    VARCHAR(255),
    tool_call_id VARCHAR(255),
    trace_id     UUID REFERENCES traces(id) ON DELETE SET NULL,
    span_id      UUID REFERENCES spans(id) ON DELETE SET NULL,
    metadata     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, run_id, seq)
);

CREATE INDEX idx_run_timeline_run_seq ON run_timeline_items(tenant_id, run_id, seq);
CREATE INDEX idx_run_timeline_session_time ON run_timeline_items(tenant_id, session_key, created_at DESC);
CREATE INDEX idx_run_timeline_trace ON run_timeline_items(tenant_id, trace_id) WHERE trace_id IS NOT NULL;

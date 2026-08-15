-- Durable agent run records. One row per agent run — the run-state machine
-- backing (pending → running → compacting → completed/failed/cancelled), with
-- heartbeat + stale-recovery support. Distinct from run_timeline_items (event
-- journal): this is the authoritative run record.
CREATE TABLE agent_runs (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    run_id       TEXT NOT NULL,
    session_key  VARCHAR(500) NOT NULL,
    agent_id     UUID REFERENCES agents(id) ON DELETE SET NULL,
    user_id      VARCHAR(255),
    channel      VARCHAR(50),
    chat_id      VARCHAR(255),
    status       VARCHAR(40) NOT NULL DEFAULT 'pending',
    attempt      INT NOT NULL DEFAULT 1,
    checkpoint   JSONB,
    heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    error        TEXT,
    metadata     JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, run_id)
);

CREATE INDEX idx_agent_runs_tenant_status ON agent_runs(tenant_id, status);
CREATE INDEX idx_agent_runs_session ON agent_runs(tenant_id, session_key, created_at DESC);
-- Persist subagent task lifecycle for audit trail, cost attribution, and restart recovery.
CREATE TABLE IF NOT EXISTS subagent_tasks (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    parent_agent_key  VARCHAR(255) NOT NULL,
    session_key       VARCHAR(500),
    subject           VARCHAR(255) NOT NULL,
    description       TEXT NOT NULL,
    status            VARCHAR(20) NOT NULL DEFAULT 'running',
    result            TEXT,
    depth             INT NOT NULL DEFAULT 1,
    model             VARCHAR(255),
    provider          VARCHAR(255),
    iterations        INT NOT NULL DEFAULT 0,
    input_tokens      BIGINT NOT NULL DEFAULT 0,
    output_tokens     BIGINT NOT NULL DEFAULT 0,
    origin_channel    VARCHAR(50),
    origin_chat_id    VARCHAR(255),
    origin_peer_kind  VARCHAR(20),
    origin_user_id    VARCHAR(255),
    spawned_by        UUID,
    completed_at      TIMESTAMPTZ,
    archived_at       TIMESTAMPTZ,
    metadata          JSONB NOT NULL DEFAULT '{}',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Primary lookup: roster by parent + status.
CREATE INDEX idx_subagent_tasks_parent_status
    ON subagent_tasks(tenant_id, parent_agent_key, status);

-- Session-scoped lookup.
CREATE INDEX idx_subagent_tasks_session
    ON subagent_tasks(session_key) WHERE session_key IS NOT NULL;

-- Time-based audit & cleanup.
CREATE INDEX idx_subagent_tasks_created
    ON subagent_tasks(tenant_id, created_at DESC);

-- Flexible metadata queries.
CREATE INDEX idx_subagent_tasks_metadata_gin
    ON subagent_tasks USING GIN (metadata);

-- Archival candidates.
CREATE INDEX idx_subagent_tasks_archive
    ON subagent_tasks(status, completed_at)
    WHERE status IN ('completed', 'failed', 'cancelled') AND archived_at IS NULL;

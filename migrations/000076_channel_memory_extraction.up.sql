CREATE TABLE IF NOT EXISTS channel_memory_extraction_runs (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_instance_id UUID NOT NULL REFERENCES channel_instances(id) ON DELETE CASCADE,
    channel_name VARCHAR(255) NOT NULL,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id VARCHAR(255) NOT NULL DEFAULT '',
    history_key VARCHAR(255) NOT NULL,
    trigger VARCHAR(32) NOT NULL DEFAULT 'scheduled',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    source_start_id VARCHAR(255) NOT NULL DEFAULT '',
    source_end_id VARCHAR(255) NOT NULL DEFAULT '',
    source_start_at TIMESTAMPTZ,
    source_end_at TIMESTAMPTZ,
    message_count INTEGER NOT NULL DEFAULT 0,
    redaction_count INTEGER NOT NULL DEFAULT 0,
    redaction_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    item_count INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, channel_instance_id, history_key, source_start_id, source_end_id)
);

CREATE INDEX IF NOT EXISTS idx_channel_memory_runs_channel
    ON channel_memory_extraction_runs(tenant_id, channel_instance_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_channel_memory_runs_status
    ON channel_memory_extraction_runs(tenant_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS channel_memory_extraction_items (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    run_id UUID NOT NULL REFERENCES channel_memory_extraction_runs(id) ON DELETE CASCADE,
    channel_instance_id UUID NOT NULL REFERENCES channel_instances(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id VARCHAR(255) NOT NULL DEFAULT '',
    item_hash VARCHAR(128) NOT NULL,
    item_type VARCHAR(64) NOT NULL,
    summary TEXT NOT NULL,
    topics JSONB NOT NULL DEFAULT '[]'::jsonb,
    entities JSONB NOT NULL DEFAULT '[]'::jsonb,
    confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
    source_id VARCHAR(255) NOT NULL DEFAULT '',
    status VARCHAR(32) NOT NULL DEFAULT 'pending_review',
    approved_by VARCHAR(255) NOT NULL DEFAULT '',
    approved_at TIMESTAMPTZ,
    rejected_by VARCHAR(255) NOT NULL DEFAULT '',
    rejected_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    written_at TIMESTAMPTZ,
    episodic_id VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, run_id, item_hash)
);

CREATE INDEX IF NOT EXISTS idx_channel_memory_items_channel_status
    ON channel_memory_extraction_items(tenant_id, channel_instance_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_channel_memory_items_run
    ON channel_memory_extraction_items(tenant_id, run_id);

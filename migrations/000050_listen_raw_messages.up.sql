CREATE TABLE listen_raw_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    channel_name VARCHAR(100) NOT NULL,
    chat_id VARCHAR(255) NOT NULL,
    chat_name VARCHAR(255) NOT NULL DEFAULT '',
    graph_id VARCHAR(255) NOT NULL,
    sender VARCHAR(255) NOT NULL DEFAULT '',
    sender_id VARCHAR(255) NOT NULL DEFAULT '',
    body TEXT NOT NULL DEFAULT '',
    msg_timestamp TIMESTAMPTZ NOT NULL,
    agent_id UUID NOT NULL,
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processed_at TIMESTAMPTZ
);

CREATE INDEX idx_listen_raw_agent_chat ON listen_raw_messages(agent_id, chat_id, created_at);
CREATE INDEX idx_listen_raw_pending ON listen_raw_messages(processed_at) WHERE processed_at IS NULL;
CREATE INDEX idx_listen_raw_tenant ON listen_raw_messages(tenant_id);

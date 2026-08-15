-- 000005_phase4: Agent handoff routing table

CREATE TABLE IF NOT EXISTS handoff_routes (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    channel        VARCHAR(50) NOT NULL,
    chat_id        VARCHAR(255) NOT NULL,
    from_agent_key VARCHAR(255) NOT NULL,
    to_agent_key   VARCHAR(255) NOT NULL,
    reason         TEXT,
    created_by     VARCHAR(255),
    created_at     TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(channel, chat_id)
);

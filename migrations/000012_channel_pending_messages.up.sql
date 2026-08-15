-- Channel pending messages: persists group chat messages when bot is NOT mentioned.
-- Used to provide conversational context when bot IS mentioned.
-- Supports LLM-based compaction (is_summary rows) and 7-day TTL cleanup.

CREATE TABLE channel_pending_messages (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    channel_name    VARCHAR(100) NOT NULL,
    history_key     VARCHAR(200) NOT NULL,
    sender          VARCHAR(255) NOT NULL,
    sender_id       VARCHAR(255) NOT NULL DEFAULT '',
    body            TEXT NOT NULL,
    platform_msg_id VARCHAR(100) NOT NULL DEFAULT '',
    is_summary      BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_channel_pending_messages_lookup
    ON channel_pending_messages (channel_name, history_key, created_at);

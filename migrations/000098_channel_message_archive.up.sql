-- Append-only archive of group capture.
--
-- channel_pending_messages is a live buffer: rows leave it when the bot is
-- mentioned (the buffer is handed to the agent, then dropped) and when LLM
-- compaction replaces old rows with a summary. Both paths deleted the only
-- stored copy of the raw group messages, so anything that had not already been
-- read out of the buffer was unrecoverable.
--
-- Every row is copied here before it is deleted from the buffer. Rows keep
-- their original id so replaying a delete cannot duplicate them.

CREATE TABLE channel_message_archive (
    id                 UUID PRIMARY KEY,
    channel_name       VARCHAR(100) NOT NULL,
    history_key        VARCHAR(200) NOT NULL,
    parent_history_key VARCHAR(200) NOT NULL DEFAULT '',
    sender             VARCHAR(255) NOT NULL,
    sender_id          VARCHAR(255) NOT NULL DEFAULT '',
    body               TEXT NOT NULL,
    platform_msg_id    VARCHAR(100) NOT NULL DEFAULT '',
    is_summary         BOOLEAN NOT NULL DEFAULT false,
    created_at         TIMESTAMPTZ NOT NULL,
    updated_at         TIMESTAMPTZ NOT NULL,
    tenant_id          UUID NOT NULL REFERENCES tenants(id),
    archived_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    archive_reason     VARCHAR(20) NOT NULL
);

-- Primary read path: replay one group in chronological order.
CREATE INDEX idx_channel_message_archive_lookup
    ON channel_message_archive (tenant_id, channel_name, history_key, created_at);

-- Retention sweeps and "what was archived recently" queries.
CREATE INDEX idx_channel_message_archive_archived_at
    ON channel_message_archive (tenant_id, archived_at);

ALTER TABLE channel_pending_messages
    ADD COLUMN IF NOT EXISTS parent_history_key VARCHAR(200) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_channel_pending_messages_parent
    ON channel_pending_messages (channel_name, parent_history_key)
    WHERE parent_history_key <> '';

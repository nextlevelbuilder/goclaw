DROP INDEX IF EXISTS idx_channel_pending_messages_parent;

ALTER TABLE channel_pending_messages
    DROP COLUMN IF EXISTS parent_history_key;

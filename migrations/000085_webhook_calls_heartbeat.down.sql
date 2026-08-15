DROP INDEX IF EXISTS idx_webhook_calls_running_heartbeat;
ALTER TABLE webhook_calls DROP COLUMN IF EXISTS last_heartbeat_at;

-- Heartbeat (lease renewal) column for async webhook call leases.
-- Lets a long-running agent renew its lease so ReclaimStale won't reclaim a live worker.
ALTER TABLE webhook_calls ADD COLUMN last_heartbeat_at timestamptz;

-- Backfill running rows so they are not reclaimed immediately after deploy
-- (their lease is still valid until staleRunningWindow elapses from started_at).
UPDATE webhook_calls SET last_heartbeat_at = started_at WHERE status = 'running';

-- Partial index: ReclaimStale scans only running rows by heartbeat.
CREATE INDEX IF NOT EXISTS idx_webhook_calls_running_heartbeat
    ON webhook_calls (status, last_heartbeat_at) WHERE status = 'running';

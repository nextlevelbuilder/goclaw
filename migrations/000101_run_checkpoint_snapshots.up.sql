-- Append-only checkpoint-snapshot history for durable agent runs. One row per
-- versioned pipeline checkpoint; agent_runs.checkpoint holds the latest while
-- this table keeps the full history so a paused run can be replayed ("time
-- travel") from ANY earlier snapshot seq. Snapshot holds MarshalCheckpoint
-- output, treated as opaque JSONB by the store layer.
CREATE TABLE run_checkpoint_snapshots (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    run_id     TEXT NOT NULL,
    seq        INT NOT NULL,
    snapshot   JSONB NOT NULL DEFAULT '{}'::jsonb,
    status     VARCHAR(40) NOT NULL DEFAULT 'paused',
    iteration  INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Replay reads snapshots newest-first per run, scoped to the tenant.
CREATE INDEX idx_run_checkpoint_snapshots_tenant_run_seq
    ON run_checkpoint_snapshots(tenant_id, run_id, seq DESC);
-- Tenant-scoped listing of snapshot history, newest first.
CREATE INDEX idx_run_checkpoint_snapshots_tenant_created
    ON run_checkpoint_snapshots(tenant_id, created_at DESC);
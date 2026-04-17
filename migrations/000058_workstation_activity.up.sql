-- Migration 000058: workstation_activity — rolling audit log for exec events.
-- Append-only; pruned nightly via Prune(before) store method.
-- cmd_preview: first 200 chars of command (redacted secrets); cmd_hash: sha256 for forensics.

CREATE TABLE IF NOT EXISTS workstation_activity (
    id             UUID PRIMARY KEY,
    tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    workstation_id UUID NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
    agent_id       VARCHAR(255) NOT NULL DEFAULT '',
    action         VARCHAR(20)  NOT NULL,  -- 'exec' | 'deny'
    cmd_hash       VARCHAR(64)  NOT NULL DEFAULT '',
    cmd_preview    VARCHAR(200) NOT NULL DEFAULT '',
    exit_code      INTEGER,
    duration_ms    INTEGER,
    deny_reason    VARCHAR(200) NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_ws_activity_ws_time     ON workstation_activity(workstation_id, created_at DESC);
CREATE INDEX idx_ws_activity_tenant_time ON workstation_activity(tenant_id, created_at DESC);
CREATE INDEX idx_ws_activity_retention   ON workstation_activity(created_at);

-- Migration 000057: workstation_permissions (allowlist per workstation).
-- Default-deny: no matching enabled pattern → deny.
-- Pattern matches against argv[0] binary name only (not full command string).
-- Seeding happens inside WorkstationStore.Create transaction (H5 fix).

CREATE TABLE IF NOT EXISTS workstation_permissions (
    id             UUID PRIMARY KEY,
    workstation_id UUID NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
    tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    pattern        VARCHAR(500) NOT NULL,  -- binary name or prefix-glob, e.g. "git", "python*"
    enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    created_by     VARCHAR(255) NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (workstation_id, pattern)
);

-- Partial index: only index enabled entries (used in PermissionChecker.loadAllowlist).
CREATE INDEX idx_workstation_perms_ws ON workstation_permissions(workstation_id) WHERE enabled = TRUE;
CREATE INDEX idx_workstation_perms_tenant ON workstation_permissions(tenant_id);

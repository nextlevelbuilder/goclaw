CREATE TABLE IF NOT EXISTS workstations (
    id              UUID PRIMARY KEY,
    workstation_key VARCHAR(100) NOT NULL,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name            VARCHAR(255) NOT NULL,
    backend_type    VARCHAR(20) NOT NULL CHECK (backend_type IN ('ssh','docker')),
    metadata        BYTEA NOT NULL,
    default_cwd     VARCHAR(500) NOT NULL DEFAULT '',
    default_env     BYTEA NOT NULL,
    active          BOOLEAN NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by      VARCHAR(255) NOT NULL DEFAULT '',
    UNIQUE (tenant_id, workstation_key)
);
CREATE INDEX IF NOT EXISTS idx_workstations_tenant_active
    ON workstations(tenant_id, active) WHERE active = TRUE;

CREATE TABLE IF NOT EXISTS agent_workstation_links (
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    workstation_id  UUID NOT NULL REFERENCES workstations(id) ON DELETE CASCADE,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    is_default      BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agent_id, workstation_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_workstation_default
    ON agent_workstation_links(agent_id) WHERE is_default = TRUE;
CREATE INDEX IF NOT EXISTS idx_agent_workstation_tenant ON agent_workstation_links(tenant_id);

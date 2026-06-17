-- Per-agent credentials for secure CLI binaries.
-- Stores typed secret material separately from secure_cli_agent_grants policy.
CREATE UNIQUE INDEX IF NOT EXISTS idx_secure_cli_binaries_id_tenant
    ON secure_cli_binaries(id, tenant_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_agents_id_tenant
    ON agents(id, tenant_id);

CREATE TABLE secure_cli_agent_credentials (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    binary_id       UUID NOT NULL,
    agent_id        UUID NOT NULL,
    encrypted_env   BYTEA NOT NULL,
    metadata        JSONB NOT NULL DEFAULT '{}',
    tenant_id       UUID NOT NULL REFERENCES tenants(id),
    credential_type TEXT NULL,
    host_scope      TEXT NULL,
    created_by      VARCHAR(255) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(binary_id, agent_id, tenant_id),
    CONSTRAINT fk_scac_binary_tenant
        FOREIGN KEY (binary_id, tenant_id) REFERENCES secure_cli_binaries(id, tenant_id) ON DELETE CASCADE,
    CONSTRAINT fk_scac_agent_tenant
        FOREIGN KEY (agent_id, tenant_id) REFERENCES agents(id, tenant_id) ON DELETE CASCADE
);

CREATE INDEX idx_scac_tenant ON secure_cli_agent_credentials(tenant_id);
CREATE INDEX idx_scac_binary ON secure_cli_agent_credentials(binary_id);
CREATE INDEX idx_scac_agent ON secure_cli_agent_credentials(agent_id);

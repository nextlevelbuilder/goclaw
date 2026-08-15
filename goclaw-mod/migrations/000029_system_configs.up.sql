-- system_configs: centralized key-value store for per-tenant system settings.
-- Each tenant has its own config entries. Falls back to master tenant at app layer.
-- Plain TEXT value (not encrypted). Use config_secrets for secrets.
CREATE TABLE IF NOT EXISTS system_configs (
    key        VARCHAR(100) NOT NULL,
    value      TEXT NOT NULL,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (key, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_system_configs_tenant ON system_configs(tenant_id);

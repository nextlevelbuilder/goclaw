-- Per-user credentials for secure CLI binaries.
-- Mirrors mcp_user_credentials pattern: user-specific env vars override binary defaults.
CREATE TABLE secure_cli_user_credentials (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    binary_id     UUID NOT NULL REFERENCES secure_cli_binaries(id) ON DELETE CASCADE,
    user_id       VARCHAR(255) NOT NULL,
    encrypted_env BYTEA NOT NULL,  -- AES-256-GCM encrypted JSON: {"GH_TOKEN":"xxx"}
    metadata      JSONB NOT NULL DEFAULT '{}',
    tenant_id     UUID NOT NULL REFERENCES tenants(id),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(binary_id, user_id, tenant_id)
);

CREATE INDEX idx_scuc_tenant ON secure_cli_user_credentials(tenant_id);
CREATE INDEX idx_scuc_binary ON secure_cli_user_credentials(binary_id);

-- Add contact_type column to channel_contacts to distinguish user vs group contacts.
-- Default "user" for backward compatibility with existing records.
ALTER TABLE channel_contacts ADD COLUMN IF NOT EXISTS contact_type VARCHAR(20) NOT NULL DEFAULT 'user';

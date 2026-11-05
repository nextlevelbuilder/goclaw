-- Secret vault: encrypted secrets that agents can use at execution time without
-- the secret value ever entering LLM context.
--
-- Separate from secure_cli_*: those tables are binary-scoped env injection for
-- exec; the vault is domain-scoped, write-only, and carries login credentials.
--
-- encrypted_payload holds AES-256-GCM ciphertext (aes-gcm: prefix); the store
-- refuses to write or read anything else. allowed_domains is never empty: a
-- secret with no domain binding can be sent anywhere.
--
-- secret_vault_audit has no foreign key on entry_id so the trail survives
-- deleting an entry. It stores the entry name for readability and never the
-- secret value or any derivative.

CREATE TABLE secret_vault_entries (
    id                UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id         UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name              VARCHAR(64) NOT NULL,
    kind              VARCHAR(16) NOT NULL CHECK (kind IN ('api_key', 'login')),
    encrypted_payload TEXT NOT NULL,
    allowed_domains   JSONB NOT NULL CHECK (jsonb_typeof(allowed_domains) = 'array' AND jsonb_array_length(allowed_domains) > 0),
    require_approval  BOOLEAN NOT NULL DEFAULT false,
    hints             TEXT NOT NULL DEFAULT '',
    created_by        VARCHAR(255) NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX idx_secret_vault_entries_tenant ON secret_vault_entries(tenant_id);

CREATE TABLE secret_vault_grants (
    entry_id   UUID NOT NULL REFERENCES secret_vault_entries(id) ON DELETE CASCADE,
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (entry_id, agent_id)
);

CREATE INDEX idx_secret_vault_grants_agent ON secret_vault_grants(agent_id, tenant_id);

CREATE TABLE secret_vault_audit (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    entry_id    UUID NOT NULL,
    entry_name  VARCHAR(64) NOT NULL,
    agent_id    UUID,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    action      VARCHAR(16) NOT NULL,
    target_host VARCHAR(255) NOT NULL DEFAULT '',
    outcome     VARCHAR(255) NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_secret_vault_audit_entry ON secret_vault_audit(tenant_id, entry_id, created_at DESC);

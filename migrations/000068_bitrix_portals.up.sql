-- Migration 000068: Bitrix24 portal OAuth state
-- Creates bitrix_portals table that stores per-tenant OAuth credentials and
-- refresh state for a Bitrix24 portal. Multiple bitrix24 channels (chatbots)
-- can share the same portal row via a portal reference on the channel
-- instance config.
--
-- `credentials` (client_id/client_secret) and `state` (access/refresh tokens,
-- member_id, app_token, registered_bots, media_folders) are both stored as
-- AES-256-GCM ciphertext via internal/crypto/aes.go. Empty encryption key
-- stores plaintext with a warn log (per crypto.Encrypt contract).

CREATE TABLE IF NOT EXISTS bitrix_portals (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name         VARCHAR(100) NOT NULL,
    domain       VARCHAR(255) NOT NULL,
    -- credentials: AES-GCM ciphertext of {client_id, client_secret}
    credentials  BYTEA,
    -- state: AES-GCM ciphertext of BitrixPortalState JSON
    state        BYTEA,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One portal name per tenant. Different tenants may reuse the same name.
CREATE UNIQUE INDEX IF NOT EXISTS idx_bitrix_portals_tenant_name
    ON bitrix_portals (tenant_id, name);

-- Incoming install/event callbacks resolve by domain before tenant scope is known.
CREATE UNIQUE INDEX IF NOT EXISTS idx_bitrix_portals_domain
    ON bitrix_portals (LOWER(TRIM(domain)));

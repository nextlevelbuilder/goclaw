-- Migration 000069: selected browser cookie sync
-- Stores user-selected cookies for server-side browser contexts. Values are
-- AES-256-GCM ciphertext from internal/crypto/aes.go; cookie scope is tenant,
-- user, and agent to prevent cross-principal reuse.

CREATE TABLE IF NOT EXISTS browser_cookies (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id         VARCHAR(255) NOT NULL,
    agent_id        VARCHAR(255) NOT NULL,
    domain          TEXT NOT NULL,
    name            TEXT NOT NULL,
    path            TEXT NOT NULL DEFAULT '/',
    encrypted_value TEXT NOT NULL,
    secure          BOOLEAN NOT NULL DEFAULT FALSE,
    http_only       BOOLEAN NOT NULL DEFAULT FALSE,
    same_site       VARCHAR(32) NOT NULL DEFAULT '',
    expires_at      TIMESTAMPTZ,
    source          VARCHAR(64) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT browser_cookies_domain_not_empty CHECK (TRIM(domain) <> ''),
    CONSTRAINT browser_cookies_name_not_empty CHECK (TRIM(name) <> ''),
    CONSTRAINT browser_cookies_path_not_empty CHECK (TRIM(path) <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_browser_cookies_scope_unique
    ON browser_cookies (tenant_id, user_id, agent_id, domain, path, name);

CREATE INDEX IF NOT EXISTS idx_browser_cookies_scope_domain
    ON browser_cookies (tenant_id, user_id, agent_id, domain);

CREATE INDEX IF NOT EXISTS idx_browser_cookies_expires_at
    ON browser_cookies (expires_at);

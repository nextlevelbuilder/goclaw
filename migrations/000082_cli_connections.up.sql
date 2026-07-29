-- Promote "connected CLI agents" (Claude Code, Codex, Aider, Gemini CLI, …) from
-- a per-agent attribute to a tenant-level resource.
--
-- BEFORE: each connection lived inside `agents.connected_agents` (JSONB), so a
-- connection was trapped in whichever agent it was attached to — connecting
-- Claude Code to one agent left every other agent unable to delegate, and the
-- credential was keyed (agent_id, connection_id) so it could not be shared.
--
-- AFTER: connections live here, scoped to a tenant, and EVERY agent in that
-- tenant may delegate to any enabled connection — no per-agent wiring and no
-- grants table (deliberate product decision: availability is tenant-wide).
-- Credentials move to per-USER rows so a user's own subscription token / API key
-- stays theirs even though the connection itself is shared.
--
-- Expand/contract: `agents.connected_agents` is intentionally LEFT IN PLACE and
-- still readable this release. The backfill below copies existing rows forward;
-- a later release drops the column once no reader remains.

BEGIN;

-- tenant_id NULL = a global connection visible to every tenant (platform-provided),
-- mirroring the mcp_servers scoping model (migration 000058).
CREATE TABLE IF NOT EXISTS cli_connections (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID REFERENCES tenants(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,                              -- display name, e.g. "Claude Code"
    kind       TEXT NOT NULL DEFAULT 'external_cli',        -- external_cli | mcp | a2a | http
    provider   TEXT NOT NULL,                              -- claude_code | codex | aider | gemini_cli | generic
    mode       TEXT NOT NULL DEFAULT 'delegate',           -- delegate | engine
    endpoint   TEXT,
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    -- Provider-specific settings. For provider='generic' this carries the whole
    -- command contract ({binary, task_args, cred_env, output}) so an arbitrary CLI
    -- can be wired with no code change.
    config     JSONB,
    created_by VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Two partial unique indexes (the 000058 pattern): per-tenant names unique within
-- their tenant, global names unique across the table, enforced independently.
CREATE UNIQUE INDEX IF NOT EXISTS idx_cli_connections_tenant_name
    ON cli_connections (tenant_id, lower(name))
    WHERE tenant_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_cli_connections_global_name
    ON cli_connections (lower(name))
    WHERE tenant_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_cli_connections_tenant_enabled
    ON cli_connections (tenant_id, enabled);

-- Per-USER credentials for a shared connection (BYOK). user_id = '' means a
-- tenant-shared credential, so a tenant admin can provide one key for everyone
-- while individual users may still override it with their own.
--
-- `inject` keeps the same contract as the old table so new CLIs slot in without a
-- schema change: "env:VAR" (e.g. env:CLAUDE_CODE_OAUTH_TOKEN) or "file:PATH".
CREATE TABLE IF NOT EXISTS cli_connection_credentials (
    connection_id UUID NOT NULL REFERENCES cli_connections(id) ON DELETE CASCADE,
    user_id       VARCHAR(255) NOT NULL DEFAULT '',   -- '' = tenant-shared
    tenant_id     UUID REFERENCES tenants(id) ON DELETE CASCADE,
    cred_type     TEXT NOT NULL,                      -- api_key | oauth
    inject        TEXT NOT NULL,                      -- env:VAR | file:PATH
    secret_enc    TEXT NOT NULL,                      -- AES-256-GCM (crypto.Encrypt)
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (connection_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_cli_connection_credentials_tenant
    ON cli_connection_credentials (tenant_id);

-- ── Backfill ─────────────────────────────────────────────────────────────────
-- Expand each agent's connected_agents JSONB into tenant-level rows. Multiple
-- agents that were connected to "the same" CLI collapse into ONE connection per
-- (tenant, provider, name) — which is the whole point of the change.
--
-- ALL of the old connection ids that collapsed into a row are kept in
-- `config->'legacy_ids'` (an array, not a single value — several agents' ids can
-- map to one connection). That lets credentials be matched below and lets a warm
-- sandbox keyed on an old id still be correlated in logs.
WITH expanded AS (
    SELECT a.tenant_id,
           COALESCE(NULLIF(c->>'name', ''), NULLIF(c->>'provider', ''), 'connection') AS name,
           COALESCE(NULLIF(c->>'kind', ''), 'external_cli')                           AS kind,
           COALESCE(NULLIF(c->>'provider', ''), 'claude_code')                         AS provider,
           COALESCE(NULLIF(c->>'mode', ''), 'delegate')                                AS mode,
           NULLIF(c->>'endpoint', '')                                                  AS endpoint,
           c->>'id'                                                                    AS legacy_id,
           a.created_at
      FROM agents a
      CROSS JOIN LATERAL jsonb_array_elements(a.connected_agents) AS c
     WHERE a.connected_agents IS NOT NULL
       AND jsonb_typeof(a.connected_agents) = 'array'
       AND COALESCE(c->>'id', '') <> ''
), grouped AS (
    -- One connection per (tenant, provider, name). Oldest agent's spelling wins;
    -- every legacy id is retained.
    SELECT tenant_id,
           (array_agg(name     ORDER BY created_at))[1] AS name,
           (array_agg(kind     ORDER BY created_at))[1] AS kind,
           (array_agg(provider ORDER BY created_at))[1] AS provider,
           (array_agg(mode     ORDER BY created_at))[1] AS mode,
           (array_agg(endpoint ORDER BY created_at))[1] AS endpoint,
           jsonb_agg(DISTINCT legacy_id)                AS legacy_ids,
           min(created_at)                              AS first_seen
      FROM expanded
     GROUP BY tenant_id, lower(provider), lower(name)
)
INSERT INTO cli_connections (tenant_id, name, kind, provider, mode, endpoint, config, created_at)
SELECT tenant_id, name, kind, provider, mode, endpoint,
       jsonb_build_object('legacy_ids', legacy_ids), first_seen
  FROM grouped
ON CONFLICT DO NOTHING;

-- Carry existing credentials across, matched on the COLLAPSED identity
-- (tenant + provider + name) rather than on a single legacy id — matching by
-- legacy id alone silently dropped the credentials of every agent whose id was
-- not the one retained (verified against fixtures).
--
-- CAVEAT, deliberate and unavoidable: the old table never recorded WHICH user
-- saved a secret, so per-user attribution cannot be reconstructed. If several
-- agents held different secrets for what is now one connection, only the most
-- recently updated survives, as the tenant-shared credential (user_id = '').
-- Every user keeps working immediately, and any user can save their own secret
-- afterwards to override it.
WITH old_with_meta AS (
    SELECT old.cred_type, old.inject, old.secret_enc, old.updated_at,
           a.tenant_id,
           lower(COALESCE(NULLIF(c->>'provider', ''), 'claude_code'))                        AS pkey,
           lower(COALESCE(NULLIF(c->>'name', ''), NULLIF(c->>'provider', ''), 'connection')) AS nkey
      FROM connected_agent_credentials old
      JOIN agents a ON a.id = old.agent_id
      CROSS JOIN LATERAL jsonb_array_elements(a.connected_agents) AS c
     WHERE jsonb_typeof(a.connected_agents) = 'array'
       AND COALESCE(c->>'id', '') = old.connection_id
)
INSERT INTO cli_connection_credentials (connection_id, user_id, tenant_id, cred_type, inject, secret_enc, updated_at)
SELECT DISTINCT ON (n.id)
       n.id, '', n.tenant_id, o.cred_type, o.inject, o.secret_enc, o.updated_at
  FROM old_with_meta o
  JOIN cli_connections n ON n.tenant_id IS NOT DISTINCT FROM o.tenant_id
                        AND lower(n.provider) = o.pkey
                        AND lower(n.name)     = o.nkey
 ORDER BY n.id, o.updated_at DESC
ON CONFLICT DO NOTHING;

COMMIT;

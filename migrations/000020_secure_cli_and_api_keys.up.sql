-- Secure CLI binaries: credential injection for exec tool (Direct Exec Mode).
-- Admin maps binary -> env vars; GoClaw auto-injects into child process.
CREATE TABLE secure_cli_binaries (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    binary_name     TEXT NOT NULL,                          -- display name: "gh", "gcloud"
    binary_path     TEXT,                                   -- resolved absolute path (nullable, auto-resolved at runtime)
    description     TEXT NOT NULL DEFAULT '',
    encrypted_env   BYTEA NOT NULL,                         -- AES-256-GCM encrypted JSON: {"GH_TOKEN":"xxx"}
    deny_args       JSONB NOT NULL DEFAULT '[]',            -- regex patterns: ["auth\\s+", "ssh-key"]
    deny_verbose    JSONB NOT NULL DEFAULT '[]',            -- verbose flag patterns: ["--verbose", "-v"]
    timeout_seconds INTEGER NOT NULL DEFAULT 30,
    tips            TEXT NOT NULL DEFAULT '',                -- hint injected into TOOLS.md context
    agent_id        UUID REFERENCES agents(id) ON DELETE CASCADE,  -- null = global (all agents)
    enabled         BOOLEAN NOT NULL DEFAULT true,
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_secure_cli_binary_name ON secure_cli_binaries(binary_name);
CREATE INDEX idx_secure_cli_agent_id ON secure_cli_binaries(agent_id) WHERE agent_id IS NOT NULL;
-- Unique constraint: one binary per agent (with null = global treated as a distinct scope)
CREATE UNIQUE INDEX idx_secure_cli_unique_binary_agent
    ON secure_cli_binaries(binary_name, COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'::uuid));

-- API key management: multiple keys with fine-grained scopes
CREATE TABLE api_keys (
    id            UUID PRIMARY KEY,
    name          VARCHAR(100) NOT NULL,
    prefix        VARCHAR(8)   NOT NULL,              -- first 8 chars for display identification
    key_hash      VARCHAR(64)  NOT NULL UNIQUE,       -- SHA-256 hex digest
    scopes        TEXT[]       NOT NULL DEFAULT '{}',  -- e.g. {'operator.admin','operator.read'}
    expires_at    TIMESTAMPTZ,                         -- NULL = never expires
    last_used_at  TIMESTAMPTZ,
    revoked       BOOLEAN      NOT NULL DEFAULT false,
    created_by    VARCHAR(255),                        -- user ID who created the key
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Fast lookup by hash (only active keys)
CREATE INDEX idx_api_keys_key_hash ON api_keys (key_hash) WHERE NOT revoked;

-- Fast lookup by prefix (for display/search)
CREATE INDEX idx_api_keys_prefix ON api_keys (prefix);

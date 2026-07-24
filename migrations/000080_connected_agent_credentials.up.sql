-- Per-connection credentials for connected agents (BYOK): a user's own
-- Anthropic API key or Claude subscription OAuth token, attached to a specific
-- connection inside agents.connected_agents. Kept OUT of the connected_agents
-- JSONB (which is returned to clients unmasked) and encrypted at rest.
--
-- `inject` describes how the secret reaches the sandbox exec so new CLIs slot in
-- without a schema change: "env:VAR" (e.g. env:CLAUDE_CODE_OAUTH_TOKEN /
-- env:ANTHROPIC_API_KEY) or "file:PATH" (e.g. file:.codex/auth.json).
CREATE TABLE IF NOT EXISTS connected_agent_credentials (
    agent_id      UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    connection_id TEXT NOT NULL,
    cred_type     TEXT NOT NULL,           -- api_key | oauth
    inject        TEXT NOT NULL,           -- env:VAR | file:PATH
    secret_enc    TEXT NOT NULL,           -- AES-256-GCM ciphertext (crypto.Encrypt)
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (agent_id, connection_id)
);

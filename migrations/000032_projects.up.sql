-- Projects: bind a workspace to a group chat for per-project MCP isolation.
-- Each project can override MCP server env vars without cross-contamination.

CREATE TABLE IF NOT EXISTS projects (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID REFERENCES tenants(id) ON DELETE CASCADE,
    name          VARCHAR(255) NOT NULL,
    slug          VARCHAR(100) NOT NULL,
    channel_type  VARCHAR(50),
    chat_id       VARCHAR(255),
    team_id       UUID REFERENCES agent_teams(id) ON DELETE SET NULL,
    description   TEXT,
    status        VARCHAR(20) NOT NULL DEFAULT 'active',
    created_by    VARCHAR(255) NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, slug),
    UNIQUE(channel_type, chat_id)
);

CREATE INDEX IF NOT EXISTS idx_projects_channel_chat ON projects(channel_type, chat_id) WHERE status = 'active';

-- Per-project MCP server environment variable overrides.
-- Stores non-secret config (GITHUB_REPO, BRANCH_NAME, etc.) as JSONB.
-- Secrets (tokens, passwords) must stay in mcp_servers.env (AES-256-GCM encrypted).

CREATE TABLE IF NOT EXISTS project_mcp_overrides (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    server_name   VARCHAR(255) NOT NULL,
    env_overrides JSONB NOT NULL DEFAULT '{}',
    enabled       BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, server_name)
);

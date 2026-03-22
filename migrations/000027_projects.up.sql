-- 000020_projects.up.sql
-- Project entity for per-group MCP env overrides (Project-as-a-Channel)

CREATE TABLE projects (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    name          VARCHAR(255) NOT NULL,
    slug          VARCHAR(100) NOT NULL UNIQUE,
    channel_type  VARCHAR(50),
    chat_id       VARCHAR(255),
    team_id       UUID REFERENCES agent_teams(id) ON DELETE SET NULL,
    description   TEXT,
    status        VARCHAR(20) NOT NULL DEFAULT 'active',
    created_by    VARCHAR(255) NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(channel_type, chat_id)
);

CREATE TABLE project_mcp_overrides (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    project_id    UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    server_name   VARCHAR(255) NOT NULL,
    env_overrides JSONB NOT NULL DEFAULT '{}',
    enabled       BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, server_name)
);

-- Auto-update updated_at on row modification
CREATE OR REPLACE FUNCTION update_projects_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER set_projects_updated_at
    BEFORE UPDATE ON projects
    FOR EACH ROW EXECUTE FUNCTION update_projects_updated_at();

CREATE TRIGGER set_project_mcp_overrides_updated_at
    BEFORE UPDATE ON project_mcp_overrides
    FOR EACH ROW EXECUTE FUNCTION update_projects_updated_at();

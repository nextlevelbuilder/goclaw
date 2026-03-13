-- Managed tools: uploadable code-project tools with file storage
CREATE TABLE managed_tools (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    name         VARCHAR(255) NOT NULL,
    slug         VARCHAR(255) NOT NULL UNIQUE,
    description  TEXT,
    owner_id     VARCHAR(255) NOT NULL,
    visibility   VARCHAR(10) NOT NULL DEFAULT 'private',
    version      INT NOT NULL DEFAULT 1,
    status       VARCHAR(20) NOT NULL DEFAULT 'active',
    frontmatter  JSONB NOT NULL DEFAULT '{}',
    file_path    TEXT NOT NULL,
    file_size    BIGINT NOT NULL DEFAULT 0,
    file_hash    VARCHAR(64),
    tags         TEXT[],
    is_system    BOOLEAN NOT NULL DEFAULT false,
    enabled      BOOLEAN NOT NULL DEFAULT true,
    runtime      VARCHAR(50),
    entry_point  VARCHAR(255),
    created_at   TIMESTAMPTZ DEFAULT NOW(),
    updated_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_managed_tools_owner ON managed_tools(owner_id);
CREATE INDEX idx_managed_tools_visibility ON managed_tools(visibility) WHERE status = 'active';
CREATE INDEX idx_managed_tools_slug ON managed_tools(slug);
CREATE INDEX idx_managed_tools_enabled ON managed_tools(enabled) WHERE enabled = false;

CREATE TABLE managed_tool_agent_grants (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    managed_tool_id UUID NOT NULL REFERENCES managed_tools(id) ON DELETE CASCADE,
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    pinned_version  INT,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(managed_tool_id, agent_id)
);

-- GoClaw Multi-Tenant Schema
-- Requires: pgcrypto, pgvector extensions

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "vector";

-- UUID v7 function (matching backend-go)
CREATE OR REPLACE FUNCTION uuid_generate_v7() RETURNS uuid AS $$
DECLARE
    unix_ts_ms bytea;
    uuid_bytes bytea;
BEGIN
    unix_ts_ms = substring(int8send(floor(extract(epoch from clock_timestamp()) * 1000)::bigint) from 3);
    uuid_bytes = unix_ts_ms || gen_random_bytes(10);
    uuid_bytes = set_byte(uuid_bytes, 6, (b'0111' || get_byte(uuid_bytes, 6)::bit(4))::bit(8)::int);
    uuid_bytes = set_byte(uuid_bytes, 8, (b'10' || get_byte(uuid_bytes, 8)::bit(6))::bit(8)::int);
    RETURN encode(uuid_bytes, 'hex')::uuid;
END
$$ LANGUAGE plpgsql VOLATILE;

-- ============================================================
-- 1. LLM Providers
-- ============================================================

CREATE TABLE llm_providers (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    name          VARCHAR(50) NOT NULL UNIQUE,
    display_name  VARCHAR(255),
    provider_type VARCHAR(30) NOT NULL DEFAULT 'openai_compat',
    api_base      TEXT,
    api_key       TEXT,
    enabled       BOOLEAN NOT NULL DEFAULT true,
    settings      JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    updated_at    TIMESTAMPTZ DEFAULT NOW()
);

-- ============================================================
-- 2. Agents & Access Control
-- ============================================================

CREATE TABLE agents (
    id                    UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_key             VARCHAR(100) NOT NULL UNIQUE,
    display_name          VARCHAR(255),
    owner_id              VARCHAR(255) NOT NULL,
    provider              VARCHAR(50) NOT NULL DEFAULT 'openrouter',
    model                 VARCHAR(200) NOT NULL,
    context_window        INT NOT NULL DEFAULT 200000,
    max_tool_iterations   INT NOT NULL DEFAULT 20,
    workspace             TEXT NOT NULL DEFAULT '.',
    restrict_to_workspace BOOLEAN NOT NULL DEFAULT true,
    tools_config          JSONB NOT NULL DEFAULT '{}',
    sandbox_config        JSONB,
    subagents_config      JSONB,
    memory_config         JSONB,
    compaction_config     JSONB,
    context_pruning       JSONB,
    other_config          JSONB NOT NULL DEFAULT '{}',
    is_default            BOOLEAN NOT NULL DEFAULT false,
    agent_type            VARCHAR(20) NOT NULL DEFAULT 'open',
    status                VARCHAR(20) DEFAULT 'active',
    created_at            TIMESTAMPTZ DEFAULT NOW(),
    updated_at            TIMESTAMPTZ DEFAULT NOW(),
    deleted_at            TIMESTAMPTZ
);

CREATE INDEX idx_agents_owner ON agents(owner_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_agents_status ON agents(status) WHERE deleted_at IS NULL;

CREATE TABLE agent_shares (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    role       VARCHAR(20) NOT NULL DEFAULT 'user',
    granted_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, user_id)
);

CREATE INDEX idx_agent_shares_user ON agent_shares(user_id);

-- ============================================================
-- 3. Context Files & User Profiles
-- ============================================================

CREATE TABLE agent_context_files (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    file_name  VARCHAR(255) NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, file_name)
);

CREATE TABLE user_context_files (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    file_name  VARCHAR(255) NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, user_id, file_name)
);

CREATE TABLE user_agent_profiles (
    agent_id      UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id       VARCHAR(255) NOT NULL,
    workspace     TEXT,
    first_seen_at TIMESTAMPTZ DEFAULT NOW(),
    last_seen_at  TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (agent_id, user_id)
);

CREATE TABLE user_agent_overrides (
    id       UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id  VARCHAR(255) NOT NULL,
    provider VARCHAR(50),
    model    VARCHAR(200),
    settings JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, user_id)
);

-- ============================================================
-- 4. Sessions
-- ============================================================

CREATE TABLE sessions (
    id                            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    session_key                   VARCHAR(500) NOT NULL UNIQUE,
    agent_id                      UUID REFERENCES agents(id),
    user_id                       VARCHAR(255),
    messages                      JSONB NOT NULL DEFAULT '[]',
    summary                       TEXT,
    model                         VARCHAR(200),
    provider                      VARCHAR(50),
    channel                       VARCHAR(50),
    input_tokens                  BIGINT NOT NULL DEFAULT 0,
    output_tokens                 BIGINT NOT NULL DEFAULT 0,
    compaction_count              INT NOT NULL DEFAULT 0,
    memory_flush_compaction_count INT NOT NULL DEFAULT 0,
    memory_flush_at               BIGINT DEFAULT 0,
    label                         VARCHAR(500),
    spawned_by                    VARCHAR(200),
    spawn_depth                   INT NOT NULL DEFAULT 0,
    created_at                    TIMESTAMPTZ DEFAULT NOW(),
    updated_at                    TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_sessions_agent ON sessions(agent_id);
CREATE INDEX idx_sessions_user ON sessions(user_id);
CREATE INDEX idx_sessions_updated ON sessions(updated_at DESC);

-- ============================================================
-- 5. Memory (pgvector + tsvector)
-- ============================================================

CREATE TABLE memory_documents (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id    VARCHAR(255),
    path       VARCHAR(500) NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    hash       VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_memdoc_unique ON memory_documents(agent_id, COALESCE(user_id, ''), path);
CREATE INDEX idx_memdoc_agent_user ON memory_documents(agent_id, user_id);

CREATE TABLE memory_chunks (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    document_id UUID REFERENCES memory_documents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255),
    path        TEXT NOT NULL,
    start_line  INT NOT NULL DEFAULT 0,
    end_line    INT NOT NULL DEFAULT 0,
    hash        VARCHAR(64) NOT NULL,
    text        TEXT NOT NULL,
    embedding   vector(1536),
    tsv         tsvector GENERATED ALWAYS AS (to_tsvector('simple', text)) STORED,
    -- NOTE: 'simple' config (no stemming) works correctly for Vietnamese and other non-English languages
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_mem_agent_user ON memory_chunks(agent_id, user_id);
CREATE INDEX idx_mem_global ON memory_chunks(agent_id) WHERE user_id IS NULL;
CREATE INDEX idx_mem_document ON memory_chunks(document_id);
CREATE INDEX idx_mem_tsv ON memory_chunks USING GIN(tsv);
CREATE INDEX idx_mem_vec ON memory_chunks USING hnsw(embedding vector_cosine_ops);

CREATE TABLE embedding_cache (
    hash      VARCHAR(64) NOT NULL,
    provider  VARCHAR(50) NOT NULL,
    model     VARCHAR(200) NOT NULL,
    embedding vector(1536),
    dims      INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (hash, provider, model)
);

-- ============================================================
-- 6. Skills (metadata + filesystem content)
-- ============================================================

CREATE TABLE skills (
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
    embedding    vector(1536),
    tags         TEXT[],
    created_at   TIMESTAMPTZ DEFAULT NOW(),
    updated_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_skills_owner ON skills(owner_id);
CREATE INDEX idx_skills_visibility ON skills(visibility) WHERE status = 'active';
CREATE INDEX idx_skills_slug ON skills(slug);
CREATE INDEX idx_skills_embedding ON skills USING hnsw(embedding vector_cosine_ops);
CREATE INDEX idx_skills_tags ON skills USING GIN(tags);

CREATE TABLE skill_agent_grants (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    skill_id       UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    agent_id       UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    pinned_version INT NOT NULL,
    granted_by     VARCHAR(255) NOT NULL,
    created_at     TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(skill_id, agent_id)
);

CREATE INDEX idx_skill_agent_grants_agent ON skill_agent_grants(agent_id);

CREATE TABLE skill_user_grants (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    skill_id   UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    granted_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(skill_id, user_id)
);

CREATE INDEX idx_skill_user_grants_user ON skill_user_grants(user_id);

-- ============================================================
-- 7. Cron Jobs
-- ============================================================

CREATE TABLE cron_jobs (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id         UUID REFERENCES agents(id),
    user_id          TEXT,
    name             VARCHAR(255) NOT NULL,
    enabled          BOOLEAN NOT NULL DEFAULT true,
    schedule_kind    VARCHAR(10) NOT NULL,
    cron_expression  VARCHAR(100),
    interval_ms      BIGINT,
    run_at           TIMESTAMPTZ,
    timezone         VARCHAR(50),
    payload          JSONB NOT NULL,
    delete_after_run BOOLEAN NOT NULL DEFAULT false,
    next_run_at      TIMESTAMPTZ,
    last_run_at      TIMESTAMPTZ,
    last_status      VARCHAR(20),
    last_error       TEXT,
    created_at       TIMESTAMPTZ DEFAULT NOW(),
    updated_at       TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_cron_jobs_user_id ON cron_jobs(user_id);
CREATE INDEX idx_cron_jobs_agent_user ON cron_jobs(agent_id, user_id);

CREATE TABLE cron_run_logs (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    job_id        UUID NOT NULL REFERENCES cron_jobs(id) ON DELETE CASCADE,
    agent_id      UUID REFERENCES agents(id),
    status        VARCHAR(20) NOT NULL,
    summary       TEXT,
    error         TEXT,
    duration_ms   INT,
    input_tokens  INT DEFAULT 0,
    output_tokens INT DEFAULT 0,
    ran_at        TIMESTAMPTZ DEFAULT NOW(),
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_cron_run_logs_job ON cron_run_logs(job_id, ran_at DESC);

-- ============================================================
-- 8. Pairing
-- ============================================================

CREATE TABLE pairing_requests (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    code       VARCHAR(8) NOT NULL UNIQUE,
    sender_id  VARCHAR(200) NOT NULL,
    channel    VARCHAR(255) NOT NULL,
    chat_id    VARCHAR(200) NOT NULL,
    account_id VARCHAR(100) NOT NULL DEFAULT 'default',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE paired_devices (
    id        UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    sender_id VARCHAR(200) NOT NULL,
    channel   VARCHAR(255) NOT NULL,
    chat_id   VARCHAR(200) NOT NULL,
    paired_by VARCHAR(100) NOT NULL DEFAULT 'operator',
    paired_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(sender_id, channel)
);

-- ============================================================
-- 9. LLM Tracing
-- ============================================================

CREATE TABLE traces (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id            UUID,
    user_id             VARCHAR(255),
    session_key         TEXT,
    run_id              TEXT,
    start_time          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    end_time            TIMESTAMPTZ,
    duration_ms         INT,
    name                TEXT,
    channel             VARCHAR(50),
    input_preview       TEXT,
    output_preview      TEXT,
    total_input_tokens  INT DEFAULT 0,
    total_output_tokens INT DEFAULT 0,
    total_cost          NUMERIC(12,6) DEFAULT 0,
    span_count          INT DEFAULT 0,
    llm_call_count      INT DEFAULT 0,
    tool_call_count     INT DEFAULT 0,
    status              VARCHAR(20) DEFAULT 'running',
    error               TEXT,
    metadata            JSONB,
    tags                TEXT[],
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_traces_agent_time ON traces(agent_id, created_at DESC);
CREATE INDEX idx_traces_user_time ON traces(user_id, created_at DESC) WHERE user_id IS NOT NULL;
CREATE INDEX idx_traces_session ON traces(session_key, created_at DESC) WHERE session_key IS NOT NULL;
CREATE INDEX idx_traces_status ON traces(status) WHERE status = 'error';

CREATE TABLE spans (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    trace_id       UUID NOT NULL,
    parent_span_id UUID,
    agent_id       UUID,
    span_type      VARCHAR(20) NOT NULL,
    name           TEXT,
    start_time     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    end_time       TIMESTAMPTZ,
    duration_ms    INT,
    status         VARCHAR(20) DEFAULT 'running',
    error          TEXT,
    level          VARCHAR(10) DEFAULT 'DEFAULT',
    model          VARCHAR(200),
    provider       VARCHAR(50),
    input_tokens   INT,
    output_tokens  INT,
    total_cost     NUMERIC(12,8),
    finish_reason  VARCHAR(50),
    model_params   JSONB,
    tool_name      VARCHAR(200),
    tool_call_id   VARCHAR(100),
    input_preview  TEXT,
    output_preview TEXT,
    metadata       JSONB,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_spans_trace ON spans(trace_id, start_time);
CREATE INDEX idx_spans_parent ON spans(parent_span_id) WHERE parent_span_id IS NOT NULL;
CREATE INDEX idx_spans_agent_time ON spans(agent_id, created_at DESC);
CREATE INDEX idx_spans_type ON spans(span_type, created_at DESC);
CREATE INDEX idx_spans_model ON spans(model, created_at DESC) WHERE model IS NOT NULL;
CREATE INDEX idx_spans_error ON spans(status) WHERE status = 'error';

-- ============================================================
-- 10. MCP Servers (External Tool Providers)
-- ============================================================

CREATE TABLE mcp_servers (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    name          VARCHAR(255) NOT NULL UNIQUE,
    display_name  VARCHAR(255),
    transport     VARCHAR(50) NOT NULL,            -- "stdio", "sse", "streamable-http"
    command       TEXT,                             -- stdio: command to spawn
    args          JSONB DEFAULT '[]',               -- stdio: command arguments
    url           TEXT,                             -- sse/http: server URL
    headers       JSONB DEFAULT '{}',               -- sse/http: HTTP headers
    env           JSONB DEFAULT '{}',               -- stdio: environment variables
    api_key       TEXT,                             -- encrypted (AES-256-GCM)
    tool_prefix   VARCHAR(50),                      -- optional prefix for tool names
    timeout_sec   INT DEFAULT 60,
    settings      JSONB NOT NULL DEFAULT '{}',
    enabled       BOOLEAN NOT NULL DEFAULT true,
    created_by    VARCHAR(255) NOT NULL,
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    updated_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE mcp_agent_grants (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    server_id        UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    agent_id         UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    enabled          BOOLEAN NOT NULL DEFAULT true,
    tool_allow       JSONB,                         -- ["tool1", "tool2"] (null = all)
    tool_deny        JSONB,                         -- ["dangerous_tool"]
    config_overrides JSONB,
    granted_by       VARCHAR(255) NOT NULL,
    created_at       TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(server_id, agent_id)
);

CREATE INDEX idx_mcp_agent_grants_agent ON mcp_agent_grants(agent_id);

CREATE TABLE mcp_user_grants (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    server_id        UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    user_id          VARCHAR(255) NOT NULL,
    enabled          BOOLEAN NOT NULL DEFAULT true,
    tool_allow       JSONB,
    tool_deny        JSONB,
    granted_by       VARCHAR(255) NOT NULL,
    created_at       TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(server_id, user_id)
);

CREATE INDEX idx_mcp_user_grants_user ON mcp_user_grants(user_id);

CREATE TABLE mcp_access_requests (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    server_id     UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    agent_id      UUID REFERENCES agents(id) ON DELETE CASCADE,
    user_id       VARCHAR(255),
    scope         VARCHAR(10) NOT NULL,             -- "agent" or "user"
    status        VARCHAR(20) NOT NULL DEFAULT 'pending', -- "pending", "approved", "rejected"
    reason        TEXT,
    tool_allow    JSONB,                            -- requested tool subset (null = all)
    requested_by  VARCHAR(255) NOT NULL,
    reviewed_by   VARCHAR(255),
    reviewed_at   TIMESTAMPTZ,
    review_note   TEXT,
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_mcp_requests_status ON mcp_access_requests(status) WHERE status = 'pending';
CREATE INDEX idx_mcp_requests_server ON mcp_access_requests(server_id);

-- ============================================================
-- 11. Custom Tools (Dynamic Tools from DB)
-- ============================================================

CREATE TABLE custom_tools (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    name            VARCHAR(100) NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    parameters      JSONB NOT NULL DEFAULT '{}',
    command         TEXT NOT NULL,
    working_dir     TEXT DEFAULT '',
    timeout_seconds INT DEFAULT 60,
    env             BYTEA,                               -- encrypted env vars (AES-256-GCM)
    agent_id        UUID REFERENCES agents(id) ON DELETE CASCADE,
    enabled         BOOLEAN DEFAULT TRUE,
    created_by      VARCHAR(255) NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

-- Global tools: unique name when agent_id IS NULL
CREATE UNIQUE INDEX idx_custom_tools_name_global ON custom_tools(name) WHERE agent_id IS NULL;
-- Per-agent tools: unique (name, agent_id) when agent_id IS NOT NULL
CREATE UNIQUE INDEX idx_custom_tools_name_agent ON custom_tools(name, agent_id) WHERE agent_id IS NOT NULL;
-- Fast lookup by agent
CREATE INDEX idx_custom_tools_agent ON custom_tools(agent_id) WHERE agent_id IS NOT NULL;

-- ============================================================
-- 12. Channel Instances
-- ============================================================

CREATE TABLE channel_instances (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    name            VARCHAR(100) NOT NULL UNIQUE,
    display_name    VARCHAR(255) DEFAULT '',
    channel_type    VARCHAR(50) NOT NULL,
    agent_id        UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    credentials     BYTEA,
    config          JSONB DEFAULT '{}',
    enabled         BOOLEAN DEFAULT true,
    created_by      VARCHAR(255) DEFAULT '',
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_channel_instances_type ON channel_instances(channel_type);
CREATE INDEX idx_channel_instances_agent ON channel_instances(agent_id);

-- ============================================================
-- 13. Config Secrets
-- ============================================================

CREATE TABLE config_secrets (
    key         VARCHAR(100) PRIMARY KEY,
    value       BYTEA NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW()
);

-- ============================================================
-- 14. Group File Writers (Telegram group write permissions)
-- ============================================================

CREATE TABLE group_file_writers (
    agent_id     UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    group_id     VARCHAR(255) NOT NULL,
    user_id      VARCHAR(255) NOT NULL,
    display_name VARCHAR(255),
    username     VARCHAR(255),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agent_id, group_id, user_id)
);

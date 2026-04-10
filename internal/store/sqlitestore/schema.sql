-- GoClaw SQLite Schema (auto-translated from PG migrations 000001-000029)
--
-- Translation rules applied:
--   UUID          → TEXT (36-char string)
--   TIMESTAMPTZ   → TEXT (RFC3339)
--   JSONB         → TEXT (JSON string)
--   BYTEA         → BLOB
--   SERIAL/BIGSERIAL → INTEGER PRIMARY KEY AUTOINCREMENT
--   DEFAULT uuid_generate_v7() / gen_random_uuid() → removed (generated in Go)
--   DEFAULT NOW() / CURRENT_TIMESTAMP → DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
--   text[] / uuid[] → TEXT (JSON array)
--   vector(N)     → OMITTED (sqlite-vec virtual tables added separately)
--   tsvector      → OMITTED (FTS5 virtual tables added separately)
--   CREATE EXTENSION / CREATE OR REPLACE FUNCTION → OMITTED
--   CREATE INDEX USING gin / hnsw → OMITTED
--   USING btree   → removed from CREATE INDEX
--
-- Tables dropped across migrations (not included):
--   group_file_writers       (migration 23: merged into agent_config_permissions)
--   handoff_routes           (migration 26: dropped)
--   delegation_history       (migration 26: dropped)
--   team_messages            (migration 24: dropped)
--   team_workspace_files     (migration 24: dropped)
--   team_workspace_comments  (migration 24: dropped)
--   team_workspace_file_versions (migration 24: dropped)
--   team_task_attachments    (migration 24: old version dropped, new path-based version created)
--   custom_tools             (migration 27: dropped)
--
-- FK cascade constraints reflect final state after migration 23 alterations.

PRAGMA foreign_keys = ON;

-- ============================================================
-- Table: tenants
-- (created first — referenced by almost all other tables)
-- ============================================================

CREATE TABLE IF NOT EXISTS tenants (
    id         TEXT NOT NULL PRIMARY KEY,
    name       VARCHAR(255) NOT NULL,
    slug       VARCHAR(100) NOT NULL,
    status     VARCHAR(20) NOT NULL DEFAULT 'active',
    settings   TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_tenants_slug ON tenants(slug);
CREATE INDEX IF NOT EXISTS idx_tenants_status ON tenants(status) WHERE status = 'active';

-- Seed master tenant (required by all FK references)
INSERT OR IGNORE INTO tenants (id, name, slug, status)
VALUES ('0193a5b0-7000-7000-8000-000000000001', 'Master', 'master', 'active');

-- ============================================================
-- Table: tenant_users
-- ============================================================

CREATE TABLE IF NOT EXISTS tenant_users (
    id           TEXT NOT NULL PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id      VARCHAR(255) NOT NULL,
    display_name VARCHAR(255),
    role         VARCHAR(20) NOT NULL DEFAULT 'member',
    metadata     TEXT NOT NULL DEFAULT '{}',
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(tenant_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_tenant_users_user ON tenant_users(user_id);
CREATE INDEX IF NOT EXISTS idx_tenant_users_tenant ON tenant_users(tenant_id);

-- ============================================================
-- Table: llm_providers
-- ============================================================

CREATE TABLE IF NOT EXISTS llm_providers (
    id            TEXT NOT NULL PRIMARY KEY,
    name          VARCHAR(50) NOT NULL,
    display_name  VARCHAR(255),
    provider_type VARCHAR(30) NOT NULL DEFAULT 'openai_compat',
    api_base      TEXT,
    api_key       TEXT,
    enabled       BOOLEAN NOT NULL DEFAULT 1,
    settings      TEXT NOT NULL DEFAULT '{}',
    tenant_id     TEXT NOT NULL REFERENCES tenants(id),
    created_at    TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- tenant-scoped unique name (migration 27 Phase I)
CREATE UNIQUE INDEX IF NOT EXISTS idx_llm_providers_tenant_name ON llm_providers(tenant_id, name);
CREATE INDEX IF NOT EXISTS idx_llm_providers_tenant ON llm_providers(tenant_id);

-- ============================================================
-- Table: agents
-- Note: tsv (tsvector) and embedding (vector) columns omitted
-- ============================================================

CREATE TABLE IF NOT EXISTS agents (
    id                    TEXT NOT NULL PRIMARY KEY,
    agent_key             VARCHAR(100) NOT NULL,
    display_name          VARCHAR(255),
    owner_id              VARCHAR(255) NOT NULL,
    provider              VARCHAR(50) NOT NULL DEFAULT 'openrouter',
    model                 VARCHAR(200) NOT NULL,
    context_window        INT NOT NULL DEFAULT 200000,
    max_tool_iterations   INT NOT NULL DEFAULT 20,
    workspace             TEXT NOT NULL DEFAULT '.',
    restrict_to_workspace BOOLEAN NOT NULL DEFAULT 1,
    tools_config          TEXT NOT NULL DEFAULT '{}',
    sandbox_config        TEXT,
    subagents_config      TEXT,
    memory_config         TEXT,
    compaction_config     TEXT,
    context_pruning       TEXT,
    other_config          TEXT NOT NULL DEFAULT '{}',
    emoji                 TEXT NOT NULL DEFAULT '',
    agent_description     TEXT NOT NULL DEFAULT '',
    thinking_level        TEXT NOT NULL DEFAULT '',
    max_tokens            INT NOT NULL DEFAULT 0,
    self_evolve           BOOLEAN NOT NULL DEFAULT 0,
    skill_evolve          BOOLEAN NOT NULL DEFAULT 0,
    skill_nudge_interval  INT NOT NULL DEFAULT 0,
    reasoning_config      TEXT NOT NULL DEFAULT '{}',
    workspace_sharing     TEXT NOT NULL DEFAULT '{}',
    chatgpt_oauth_routing TEXT NOT NULL DEFAULT '{}',
    shell_deny_groups     TEXT NOT NULL DEFAULT '{}',
    kg_dedup_config       TEXT NOT NULL DEFAULT '{}',
    is_default            BOOLEAN NOT NULL DEFAULT 0,
    agent_type            VARCHAR(20) NOT NULL DEFAULT 'open',
    status                VARCHAR(20) DEFAULT 'active',
    frontmatter           TEXT,
    budget_monthly_cents  INTEGER,
    tenant_id             TEXT NOT NULL REFERENCES tenants(id),
    created_at            TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at            TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    deleted_at            TEXT
);

-- Final unique constraint: tenant-scoped agent_key for active agents (migration 27 Phase I)
CREATE UNIQUE INDEX IF NOT EXISTS idx_agents_tenant_agent_key_active ON agents(tenant_id, agent_key) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_agents_owner ON agents(owner_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_agents_status ON agents(status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_agents_tenant ON agents(tenant_id);
CREATE INDEX IF NOT EXISTS idx_agents_tenant_active ON agents(tenant_id) WHERE deleted_at IS NULL;

-- ============================================================
-- Table: agent_shares
-- ============================================================

CREATE TABLE IF NOT EXISTS agent_shares (
    id         TEXT NOT NULL PRIMARY KEY,
    agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    role       VARCHAR(20) NOT NULL DEFAULT 'user',
    granted_by VARCHAR(255) NOT NULL,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(agent_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_shares_user ON agent_shares(user_id);
CREATE INDEX IF NOT EXISTS idx_agent_shares_tenant ON agent_shares(tenant_id);

-- ============================================================
-- Table: agent_context_files
-- ============================================================

CREATE TABLE IF NOT EXISTS agent_context_files (
    id         TEXT NOT NULL PRIMARY KEY,
    agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    file_name  VARCHAR(255) NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(agent_id, file_name)
);

CREATE INDEX IF NOT EXISTS idx_agent_context_files_tenant ON agent_context_files(tenant_id);

-- ============================================================
-- Table: user_context_files
-- ============================================================

CREATE TABLE IF NOT EXISTS user_context_files (
    id         TEXT NOT NULL PRIMARY KEY,
    agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    file_name  VARCHAR(255) NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(agent_id, user_id, file_name)
);

CREATE INDEX IF NOT EXISTS idx_user_context_files_tenant ON user_context_files(tenant_id);

-- ============================================================
-- Table: user_agent_profiles
-- ============================================================

CREATE TABLE IF NOT EXISTS user_agent_profiles (
    agent_id      TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id       VARCHAR(255) NOT NULL,
    workspace     TEXT,
    metadata      TEXT DEFAULT '{}',
    tenant_id     TEXT NOT NULL REFERENCES tenants(id),
    first_seen_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_seen_at  TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (agent_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_user_agent_profiles_tenant ON user_agent_profiles(tenant_id);

-- ============================================================
-- Table: user_agent_overrides
-- ============================================================

CREATE TABLE IF NOT EXISTS user_agent_overrides (
    id         TEXT NOT NULL PRIMARY KEY,
    agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    provider   VARCHAR(50),
    model      VARCHAR(200),
    settings   TEXT NOT NULL DEFAULT '{}',
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(agent_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_user_agent_overrides_tenant ON user_agent_overrides(tenant_id);

-- ============================================================
-- Table: agent_teams
-- (defined before sessions/memory since they reference it)
-- ============================================================

CREATE TABLE IF NOT EXISTS agent_teams (
    id            TEXT NOT NULL PRIMARY KEY,
    name          VARCHAR(255) NOT NULL,
    lead_agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    description   TEXT,
    status        VARCHAR(20) NOT NULL DEFAULT 'active',
    settings      TEXT NOT NULL DEFAULT '{}',
    created_by    VARCHAR(255) NOT NULL,
    tenant_id     TEXT NOT NULL REFERENCES tenants(id),
    created_at    TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_agent_teams_tenant ON agent_teams(tenant_id);

-- ============================================================
-- Table: sessions
-- ============================================================

CREATE TABLE IF NOT EXISTS sessions (
    id                            TEXT NOT NULL PRIMARY KEY,
    session_key                   VARCHAR(500) NOT NULL,
    agent_id                      TEXT REFERENCES agents(id) ON DELETE CASCADE,
    user_id                       VARCHAR(255),
    messages                      TEXT NOT NULL DEFAULT '[]',
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
    metadata                      TEXT DEFAULT '{}',
    tenant_id                     TEXT NOT NULL REFERENCES tenants(id),
    team_id                       TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
    created_at                    TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at                    TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- tenant-scoped unique session_key (migration 27 Phase I)
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_tenant_session_key ON sessions(tenant_id, session_key);
CREATE INDEX IF NOT EXISTS idx_sessions_agent ON sessions(agent_id);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_tenant ON sessions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_sessions_tenant_user ON sessions(tenant_id, user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_team ON sessions(team_id) WHERE team_id IS NOT NULL;

-- ============================================================
-- Table: memory_documents
-- ============================================================

CREATE TABLE IF NOT EXISTS memory_documents (
    id         TEXT NOT NULL PRIMARY KEY,
    agent_id   TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id    VARCHAR(255),
    path       VARCHAR(500) NOT NULL,
    content    TEXT NOT NULL DEFAULT '',
    hash       VARCHAR(64) NOT NULL,
    team_id    TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
    custom_scope TEXT,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_memdoc_unique ON memory_documents(agent_id, COALESCE(user_id, ''), path);
CREATE INDEX IF NOT EXISTS idx_memdoc_agent_user ON memory_documents(agent_id, user_id);
CREATE INDEX IF NOT EXISTS idx_memdoc_team ON memory_documents(team_id) WHERE team_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_memory_documents_tenant ON memory_documents(tenant_id);

-- ============================================================
-- Table: memory_chunks
-- Note: embedding (vector) and tsv (tsvector) columns omitted
-- ============================================================

CREATE TABLE IF NOT EXISTS memory_chunks (
    id          TEXT NOT NULL PRIMARY KEY,
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    document_id TEXT REFERENCES memory_documents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255),
    path        TEXT NOT NULL,
    start_line  INT NOT NULL DEFAULT 0,
    end_line    INT NOT NULL DEFAULT 0,
    hash        VARCHAR(64) NOT NULL,
    text        TEXT NOT NULL,
    team_id     TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
    custom_scope TEXT,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    created_at  TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_mem_agent_user ON memory_chunks(agent_id, user_id);
CREATE INDEX IF NOT EXISTS idx_mem_global ON memory_chunks(agent_id) WHERE user_id IS NULL;
CREATE INDEX IF NOT EXISTS idx_mem_document ON memory_chunks(document_id);
CREATE INDEX IF NOT EXISTS idx_memchunk_team ON memory_chunks(team_id) WHERE team_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_memory_chunks_tenant ON memory_chunks(tenant_id);

-- ============================================================
-- Table: embedding_cache
-- Note: embedding (vector) column omitted
-- ============================================================

CREATE TABLE IF NOT EXISTS embedding_cache (
    hash       VARCHAR(64) NOT NULL,
    provider   VARCHAR(50) NOT NULL,
    model      VARCHAR(200) NOT NULL,
    dims       INT NOT NULL DEFAULT 0,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (hash, provider, model)
);

CREATE INDEX IF NOT EXISTS idx_embedding_cache_tenant ON embedding_cache(tenant_id);

-- ============================================================
-- Table: skills
-- Note: embedding (vector) column omitted
-- ============================================================

CREATE TABLE IF NOT EXISTS skills (
    id          TEXT NOT NULL PRIMARY KEY,
    name        VARCHAR(255) NOT NULL,
    slug        VARCHAR(255) NOT NULL,
    description TEXT,
    owner_id    VARCHAR(255) NOT NULL,
    visibility  VARCHAR(10) NOT NULL DEFAULT 'private',
    version     INT NOT NULL DEFAULT 1,
    status      VARCHAR(20) NOT NULL DEFAULT 'active',
    frontmatter TEXT NOT NULL DEFAULT '{}',
    file_path   TEXT NOT NULL,
    file_size   BIGINT NOT NULL DEFAULT 0,
    file_hash   VARCHAR(64),
    tags        TEXT,
    is_system   BOOLEAN NOT NULL DEFAULT 0,
    deps        TEXT NOT NULL DEFAULT '{}',
    enabled     BOOLEAN NOT NULL DEFAULT 1,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    created_at  TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- tenant-scoped unique slug (migration 27 Phase I)
CREATE UNIQUE INDEX IF NOT EXISTS idx_skills_tenant_slug ON skills(tenant_id, slug);
CREATE INDEX IF NOT EXISTS idx_skills_owner ON skills(owner_id);
CREATE INDEX IF NOT EXISTS idx_skills_visibility ON skills(visibility) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_skills_system ON skills(is_system) WHERE is_system = 1;
CREATE INDEX IF NOT EXISTS idx_skills_enabled ON skills(enabled) WHERE enabled = 0;
CREATE INDEX IF NOT EXISTS idx_skills_tenant ON skills(tenant_id);

-- ============================================================
-- Table: skill_agent_grants
-- ============================================================

CREATE TABLE IF NOT EXISTS skill_agent_grants (
    id             TEXT NOT NULL PRIMARY KEY,
    skill_id       TEXT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    agent_id       TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    pinned_version INT NOT NULL,
    granted_by     VARCHAR(255) NOT NULL,
    tenant_id      TEXT NOT NULL REFERENCES tenants(id),
    created_at     TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(skill_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_skill_agent_grants_agent ON skill_agent_grants(agent_id);
CREATE INDEX IF NOT EXISTS idx_skill_agent_grants_tenant ON skill_agent_grants(tenant_id);

-- ============================================================
-- Table: skill_user_grants
-- ============================================================

CREATE TABLE IF NOT EXISTS skill_user_grants (
    id         TEXT NOT NULL PRIMARY KEY,
    skill_id   TEXT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    granted_by VARCHAR(255) NOT NULL,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(skill_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_skill_user_grants_user ON skill_user_grants(user_id);
CREATE INDEX IF NOT EXISTS idx_skill_user_grants_tenant ON skill_user_grants(tenant_id);

-- ============================================================
-- Table: skill_tenant_configs
-- ============================================================

CREATE TABLE IF NOT EXISTS skill_tenant_configs (
    skill_id   TEXT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    enabled    BOOLEAN NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (skill_id, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_skill_tenant_configs_tenant ON skill_tenant_configs(tenant_id);

-- ============================================================
-- Table: cron_jobs
-- ============================================================

CREATE TABLE IF NOT EXISTS cron_jobs (
    id               TEXT NOT NULL PRIMARY KEY,
    agent_id         TEXT REFERENCES agents(id) ON DELETE CASCADE,
    user_id          TEXT,
    name             VARCHAR(255) NOT NULL,
    enabled          BOOLEAN NOT NULL DEFAULT 1,
    schedule_kind    VARCHAR(10) NOT NULL,
    cron_expression  VARCHAR(100),
    interval_ms      BIGINT,
    run_at           TEXT,
    timezone         VARCHAR(50),
    payload          TEXT NOT NULL,
    delete_after_run BOOLEAN NOT NULL DEFAULT 0,
    stateless        INTEGER NOT NULL DEFAULT 0,
    deliver          INTEGER NOT NULL DEFAULT 0,
    deliver_channel  TEXT NOT NULL DEFAULT '',
    deliver_to       TEXT NOT NULL DEFAULT '',
    wake_heartbeat   INTEGER NOT NULL DEFAULT 0,
    next_run_at      TEXT,
    last_run_at      TEXT,
    last_status      VARCHAR(20),
    last_error       TEXT,
    team_id          TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
    tenant_id        TEXT NOT NULL REFERENCES tenants(id),
    created_at       TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at       TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_cron_jobs_user_id ON cron_jobs(user_id);
CREATE INDEX IF NOT EXISTS idx_cron_jobs_agent_user ON cron_jobs(agent_id, user_id);
CREATE INDEX IF NOT EXISTS idx_cron_jobs_team ON cron_jobs(team_id) WHERE team_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_cron_jobs_tenant ON cron_jobs(tenant_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_cron_jobs_agent_tenant_name ON cron_jobs(agent_id, tenant_id, name);

-- ============================================================
-- Table: cron_run_logs
-- ============================================================

CREATE TABLE IF NOT EXISTS cron_run_logs (
    id            TEXT NOT NULL PRIMARY KEY,
    job_id        TEXT NOT NULL REFERENCES cron_jobs(id) ON DELETE CASCADE,
    agent_id      TEXT REFERENCES agents(id) ON DELETE SET NULL,
    status        VARCHAR(20) NOT NULL,
    summary       TEXT,
    error         TEXT,
    duration_ms   INT,
    input_tokens  INT DEFAULT 0,
    output_tokens INT DEFAULT 0,
    team_id       TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
    ran_at        TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_at    TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_cron_run_logs_job ON cron_run_logs(job_id, ran_at DESC);
CREATE INDEX IF NOT EXISTS idx_cron_run_logs_team ON cron_run_logs(team_id) WHERE team_id IS NOT NULL;

-- ============================================================
-- Table: pairing_requests
-- ============================================================

CREATE TABLE IF NOT EXISTS pairing_requests (
    id         TEXT NOT NULL PRIMARY KEY,
    code       VARCHAR(8) NOT NULL UNIQUE,
    sender_id  VARCHAR(200) NOT NULL,
    channel    VARCHAR(255) NOT NULL,
    chat_id    VARCHAR(200) NOT NULL,
    account_id VARCHAR(100) NOT NULL DEFAULT 'default',
    metadata   TEXT DEFAULT '{}',
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    expires_at TEXT NOT NULL,
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_pairing_requests_tenant ON pairing_requests(tenant_id);

-- ============================================================
-- Table: paired_devices
-- ============================================================

CREATE TABLE IF NOT EXISTS paired_devices (
    id         TEXT NOT NULL PRIMARY KEY,
    sender_id  VARCHAR(200) NOT NULL,
    channel    VARCHAR(255) NOT NULL,
    chat_id    VARCHAR(200) NOT NULL,
    paired_by  VARCHAR(100) NOT NULL DEFAULT 'operator',
    metadata   TEXT DEFAULT '{}',
    expires_at TEXT,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    paired_at  TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- tenant-scoped unique (migration 27 Phase I)
CREATE UNIQUE INDEX IF NOT EXISTS idx_paired_devices_tenant_sender_channel ON paired_devices(tenant_id, sender_id, channel);
CREATE INDEX IF NOT EXISTS idx_paired_devices_tenant ON paired_devices(tenant_id);

-- ============================================================
-- Table: traces
-- ============================================================

CREATE TABLE IF NOT EXISTS traces (
    id                  TEXT NOT NULL PRIMARY KEY,
    agent_id            TEXT,
    user_id             VARCHAR(255),
    session_key         TEXT,
    run_id              TEXT,
    start_time          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    end_time            TEXT,
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
    metadata            TEXT,
    tags                TEXT,
    parent_trace_id     TEXT,
    team_id             TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
    tenant_id           TEXT NOT NULL REFERENCES tenants(id),
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_traces_agent_time ON traces(agent_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_traces_user_time ON traces(user_id, created_at DESC) WHERE user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_traces_session ON traces(session_key, created_at DESC) WHERE session_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_traces_status ON traces(status) WHERE status = 'error';
CREATE INDEX IF NOT EXISTS idx_traces_parent ON traces(parent_trace_id) WHERE parent_trace_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_traces_quota ON traces(user_id, created_at DESC) WHERE parent_trace_id IS NULL AND user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_traces_start_root ON traces(start_time DESC) WHERE parent_trace_id IS NULL;
CREATE INDEX IF NOT EXISTS idx_traces_team ON traces(team_id, created_at DESC) WHERE team_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_traces_tenant ON traces(tenant_id);
CREATE INDEX IF NOT EXISTS idx_traces_tenant_time ON traces(tenant_id, created_at DESC);

-- ============================================================
-- Table: spans
-- ============================================================

CREATE TABLE IF NOT EXISTS spans (
    id             TEXT NOT NULL PRIMARY KEY,
    trace_id       TEXT NOT NULL,
    parent_span_id TEXT,
    agent_id       TEXT,
    span_type      VARCHAR(20) NOT NULL,
    name           TEXT,
    start_time     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    end_time       TEXT,
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
    model_params   TEXT,
    tool_name      VARCHAR(200),
    tool_call_id   VARCHAR(100),
    input_preview  TEXT,
    output_preview TEXT,
    metadata       TEXT,
    team_id        TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
    tenant_id      TEXT NOT NULL REFERENCES tenants(id),
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_spans_trace ON spans(trace_id, start_time);
CREATE INDEX IF NOT EXISTS idx_spans_trace_type ON spans(trace_id, span_type);
CREATE INDEX IF NOT EXISTS idx_spans_parent ON spans(parent_span_id) WHERE parent_span_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_spans_agent_time ON spans(agent_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_spans_type ON spans(span_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_spans_model ON spans(model, created_at DESC) WHERE model IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_spans_error ON spans(status) WHERE status = 'error';
CREATE INDEX IF NOT EXISTS idx_spans_team ON spans(team_id) WHERE team_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_spans_tenant ON spans(tenant_id);

-- ============================================================
-- Table: mcp_servers
-- ============================================================

CREATE TABLE IF NOT EXISTS mcp_servers (
    id           TEXT NOT NULL PRIMARY KEY,
    name         VARCHAR(255) NOT NULL,
    display_name VARCHAR(255),
    transport    VARCHAR(50) NOT NULL,
    command      TEXT,
    args         TEXT DEFAULT '[]',
    url          TEXT,
    headers      TEXT DEFAULT '{}',
    env          TEXT DEFAULT '{}',
    api_key      TEXT,
    tool_prefix  VARCHAR(50),
    timeout_sec  INT DEFAULT 60,
    settings     TEXT NOT NULL DEFAULT '{}',
    enabled      BOOLEAN NOT NULL DEFAULT 1,
    created_by   VARCHAR(255) NOT NULL,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id),
    created_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- tenant-scoped unique name (migration 27 Phase I)
CREATE UNIQUE INDEX IF NOT EXISTS idx_mcp_servers_tenant_name ON mcp_servers(tenant_id, name);
CREATE INDEX IF NOT EXISTS idx_mcp_servers_tenant ON mcp_servers(tenant_id);

-- ============================================================
-- Table: mcp_agent_grants
-- ============================================================

CREATE TABLE IF NOT EXISTS mcp_agent_grants (
    id               TEXT NOT NULL PRIMARY KEY,
    server_id        TEXT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    agent_id         TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    enabled          BOOLEAN NOT NULL DEFAULT 1,
    tool_allow       TEXT,
    tool_deny        TEXT,
    config_overrides TEXT,
    granted_by       VARCHAR(255) NOT NULL,
    tenant_id        TEXT NOT NULL REFERENCES tenants(id),
    created_at       TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(server_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_mcp_agent_grants_agent ON mcp_agent_grants(agent_id);
CREATE INDEX IF NOT EXISTS idx_mcp_agent_grants_tenant ON mcp_agent_grants(tenant_id);

-- ============================================================
-- Table: mcp_user_grants
-- ============================================================

CREATE TABLE IF NOT EXISTS mcp_user_grants (
    id         TEXT NOT NULL PRIMARY KEY,
    server_id  TEXT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    enabled    BOOLEAN NOT NULL DEFAULT 1,
    tool_allow TEXT,
    tool_deny  TEXT,
    granted_by VARCHAR(255) NOT NULL,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(server_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_mcp_user_grants_user ON mcp_user_grants(user_id);
CREATE INDEX IF NOT EXISTS idx_mcp_user_grants_tenant ON mcp_user_grants(tenant_id);

-- ============================================================
-- Table: mcp_access_requests
-- ============================================================

CREATE TABLE IF NOT EXISTS mcp_access_requests (
    id           TEXT NOT NULL PRIMARY KEY,
    server_id    TEXT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    agent_id     TEXT REFERENCES agents(id) ON DELETE CASCADE,
    user_id      VARCHAR(255),
    scope        VARCHAR(10) NOT NULL,
    status       VARCHAR(20) NOT NULL DEFAULT 'pending',
    reason       TEXT,
    tool_allow   TEXT,
    requested_by VARCHAR(255) NOT NULL,
    reviewed_by  VARCHAR(255),
    reviewed_at  TEXT,
    review_note  TEXT,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id),
    created_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_mcp_requests_status ON mcp_access_requests(status) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_mcp_requests_server ON mcp_access_requests(server_id);
CREATE INDEX IF NOT EXISTS idx_mcp_access_requests_tenant ON mcp_access_requests(tenant_id);

-- ============================================================
-- Table: mcp_user_credentials
-- ============================================================

CREATE TABLE IF NOT EXISTS mcp_user_credentials (
    id         TEXT NOT NULL PRIMARY KEY,
    server_id  TEXT NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    api_key    TEXT,
    headers    BLOB,
    env        BLOB,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(server_id, user_id, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_mcp_user_credentials_tenant ON mcp_user_credentials(tenant_id);
CREATE INDEX IF NOT EXISTS idx_mcp_user_credentials_server ON mcp_user_credentials(server_id);

-- ============================================================
-- Table: channel_instances
-- ============================================================

CREATE TABLE IF NOT EXISTS channel_instances (
    id           TEXT NOT NULL PRIMARY KEY,
    name         VARCHAR(100) NOT NULL,
    display_name VARCHAR(255) DEFAULT '',
    channel_type VARCHAR(50) NOT NULL,
    agent_id     TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    credentials  BLOB,
    config       TEXT DEFAULT '{}',
    enabled      BOOLEAN DEFAULT 1,
    created_by   VARCHAR(255) DEFAULT '',
    tenant_id    TEXT NOT NULL REFERENCES tenants(id),
    created_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- tenant-scoped unique name (migration 27 Phase I)
CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_instances_tenant_name ON channel_instances(tenant_id, name);
CREATE INDEX IF NOT EXISTS idx_channel_instances_type ON channel_instances(channel_type);
CREATE INDEX IF NOT EXISTS idx_channel_instances_agent ON channel_instances(agent_id);
CREATE INDEX IF NOT EXISTS idx_channel_instances_tenant ON channel_instances(tenant_id);

-- ============================================================
-- Table: config_secrets
-- PK changed to (key, tenant_id) in migration 27 Phase I
-- ============================================================

CREATE TABLE IF NOT EXISTS config_secrets (
    key        VARCHAR(100) NOT NULL,
    value      BLOB NOT NULL,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    created_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (key, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_config_secrets_tenant ON config_secrets(tenant_id);

-- ============================================================
-- Table: agent_links
-- ============================================================

CREATE TABLE IF NOT EXISTS agent_links (
    id              TEXT NOT NULL PRIMARY KEY,
    source_agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    target_agent_id TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    direction       VARCHAR(20) NOT NULL DEFAULT 'outbound',
    description     TEXT,
    max_concurrent  INT NOT NULL DEFAULT 3,
    settings        TEXT NOT NULL DEFAULT '{}',
    status          VARCHAR(20) NOT NULL DEFAULT 'active',
    created_by      VARCHAR(255) NOT NULL,
    team_id         TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    created_at      TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(source_agent_id, target_agent_id),
    CHECK (source_agent_id != target_agent_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_links_source ON agent_links(source_agent_id) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_agent_links_target ON agent_links(target_agent_id) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_agent_links_tenant ON agent_links(tenant_id);

-- ============================================================
-- Table: agent_team_members
-- ============================================================

CREATE TABLE IF NOT EXISTS agent_team_members (
    team_id   TEXT NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    agent_id  TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    role      VARCHAR(20) NOT NULL DEFAULT 'member',
    tenant_id TEXT NOT NULL REFERENCES tenants(id),
    joined_at TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (team_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_agent_team_members_tenant ON agent_team_members(tenant_id);

-- ============================================================
-- Table: team_tasks
-- Note: tsv (tsvector) and embedding (vector) columns omitted
--       blocked_by stored as TEXT (JSON array of UUIDs)
-- ============================================================

CREATE TABLE IF NOT EXISTS team_tasks (
    id                   TEXT NOT NULL PRIMARY KEY,
    team_id              TEXT NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    subject              VARCHAR(500) NOT NULL,
    description          TEXT,
    status               VARCHAR(20) NOT NULL DEFAULT 'pending',
    owner_agent_id       TEXT REFERENCES agents(id) ON DELETE SET NULL,
    blocked_by           TEXT NOT NULL DEFAULT '[]',
    priority             INT NOT NULL DEFAULT 0,
    result               TEXT,
    metadata             TEXT NOT NULL DEFAULT '{}',
    user_id              VARCHAR(255),
    channel              VARCHAR(50),
    task_type            VARCHAR(30) NOT NULL DEFAULT 'general',
    task_number          INT NOT NULL DEFAULT 0,
    identifier           VARCHAR(20),
    created_by_agent_id  TEXT REFERENCES agents(id) ON DELETE SET NULL,
    assignee_user_id     VARCHAR(255),
    parent_id            TEXT REFERENCES team_tasks(id) ON DELETE SET NULL,
    chat_id              VARCHAR(255) DEFAULT '',
    locked_at            TEXT,
    lock_expires_at      TEXT,
    progress_percent     INT DEFAULT 0 CHECK (progress_percent BETWEEN 0 AND 100),
    progress_step        TEXT,
    followup_at          TEXT,
    followup_count       INT NOT NULL DEFAULT 0,
    followup_max         INT NOT NULL DEFAULT 0,
    followup_message     TEXT,
    followup_channel     VARCHAR(60),
    followup_chat_id     VARCHAR(255),
    confidence_score     REAL,
    comment_count        INT NOT NULL DEFAULT 0,
    attachment_count     INT NOT NULL DEFAULT 0,
    custom_scope         TEXT,
    tenant_id            TEXT NOT NULL REFERENCES tenants(id),
    created_at           TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at           TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_team_tasks_team ON team_tasks(team_id);
CREATE INDEX IF NOT EXISTS idx_team_tasks_status ON team_tasks(team_id, status);
CREATE INDEX IF NOT EXISTS idx_team_tasks_user_scope ON team_tasks(team_id, user_id) WHERE user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tt_parent ON team_tasks(parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tt_scope ON team_tasks(team_id, channel, chat_id);
CREATE INDEX IF NOT EXISTS idx_tt_type ON team_tasks(team_id, task_type);
CREATE INDEX IF NOT EXISTS idx_tt_lock ON team_tasks(lock_expires_at) WHERE lock_expires_at IS NOT NULL AND status = 'in_progress';
CREATE UNIQUE INDEX IF NOT EXISTS idx_tt_identifier ON team_tasks(team_id, identifier) WHERE identifier IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_tt_followup ON team_tasks(followup_at) WHERE followup_at IS NOT NULL AND status = 'in_progress';
-- idx_tt_blocked_by (GIN on array) omitted: Go code handles JSON array filtering
CREATE INDEX IF NOT EXISTS idx_tt_owner_status ON team_tasks(team_id, owner_agent_id, status);
CREATE INDEX IF NOT EXISTS idx_team_tasks_tenant ON team_tasks(tenant_id);

-- ============================================================
-- Table: team_task_comments
-- ============================================================

CREATE TABLE IF NOT EXISTS team_task_comments (
    id               TEXT NOT NULL PRIMARY KEY,
    task_id          TEXT NOT NULL REFERENCES team_tasks(id) ON DELETE CASCADE,
    agent_id         TEXT REFERENCES agents(id) ON DELETE SET NULL,
    user_id          VARCHAR(255),
    content          TEXT NOT NULL,
    metadata         TEXT DEFAULT '{}',
    comment_type     VARCHAR(20) NOT NULL DEFAULT 'note',
    confidence_score REAL,
    custom_scope     TEXT,
    tenant_id        TEXT NOT NULL REFERENCES tenants(id),
    created_at       TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_ttc_task ON team_task_comments(task_id);
CREATE INDEX IF NOT EXISTS idx_team_task_comments_tenant ON team_task_comments(tenant_id);

-- ============================================================
-- Table: team_task_events
-- ============================================================

CREATE TABLE IF NOT EXISTS team_task_events (
    id         TEXT NOT NULL PRIMARY KEY,
    task_id    TEXT NOT NULL REFERENCES team_tasks(id) ON DELETE CASCADE,
    event_type VARCHAR(30) NOT NULL,
    actor_type VARCHAR(10) NOT NULL,
    actor_id   VARCHAR(255) NOT NULL,
    data       TEXT,
    custom_scope TEXT,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_tte_task ON team_task_events(task_id);
CREATE INDEX IF NOT EXISTS idx_team_task_events_tenant ON team_task_events(tenant_id);

-- ============================================================
-- Table: team_task_attachments
-- (new path-based version from migration 24)
-- ============================================================

CREATE TABLE IF NOT EXISTS team_task_attachments (
    id                   TEXT NOT NULL PRIMARY KEY,
    task_id              TEXT NOT NULL REFERENCES team_tasks(id) ON DELETE CASCADE,
    team_id              TEXT NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    chat_id              VARCHAR(255) NOT NULL DEFAULT '',
    path                 TEXT NOT NULL,
    file_size            BIGINT NOT NULL DEFAULT 0,
    mime_type            VARCHAR(100) DEFAULT '',
    created_by_agent_id  TEXT REFERENCES agents(id),
    created_by_sender_id VARCHAR(255) DEFAULT '',
    metadata             TEXT NOT NULL DEFAULT '{}',
    custom_scope         TEXT,
    tenant_id            TEXT NOT NULL REFERENCES tenants(id),
    created_at           TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(task_id, path)
);

CREATE INDEX IF NOT EXISTS idx_tta_task ON team_task_attachments(task_id);
CREATE INDEX IF NOT EXISTS idx_tta_team ON team_task_attachments(team_id);
CREATE INDEX IF NOT EXISTS idx_team_task_attachments_tenant ON team_task_attachments(tenant_id);

-- ============================================================
-- Table: team_user_grants
-- ============================================================

CREATE TABLE IF NOT EXISTS team_user_grants (
    id         TEXT NOT NULL PRIMARY KEY,
    team_id    TEXT NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    role       VARCHAR(50) NOT NULL DEFAULT 'viewer',
    granted_by VARCHAR(255),
    tenant_id  TEXT NOT NULL REFERENCES tenants(id),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(team_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_team_user_grants_user ON team_user_grants(user_id);
CREATE INDEX IF NOT EXISTS idx_team_user_grants_team ON team_user_grants(team_id);
CREATE INDEX IF NOT EXISTS idx_team_user_grants_tenant ON team_user_grants(tenant_id);

-- ============================================================
-- Table: kg_entities
-- Note: embedding (vector) column omitted
-- ============================================================

CREATE TABLE IF NOT EXISTS kg_entities (
    id          TEXT NOT NULL PRIMARY KEY,
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    external_id VARCHAR(255) NOT NULL,
    name        TEXT NOT NULL,
    entity_type VARCHAR(100) NOT NULL,
    description TEXT DEFAULT '',
    properties  TEXT DEFAULT '{}',
    source_id   VARCHAR(255) DEFAULT '',
    confidence  REAL NOT NULL DEFAULT 1.0,
    team_id     TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    created_at  TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    valid_from  TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    valid_until TEXT,
    UNIQUE(agent_id, user_id, external_id)
);

CREATE INDEX IF NOT EXISTS idx_kg_entities_scope ON kg_entities(agent_id, user_id);
CREATE INDEX IF NOT EXISTS idx_kg_entities_type ON kg_entities(agent_id, user_id, entity_type);
CREATE INDEX IF NOT EXISTS idx_kg_entities_current ON kg_entities(agent_id, user_id) WHERE valid_until IS NULL;
CREATE INDEX IF NOT EXISTS idx_kg_entities_team ON kg_entities(team_id) WHERE team_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_kg_entities_tenant ON kg_entities(tenant_id);

-- ============================================================
-- Table: kg_relations
-- ============================================================

CREATE TABLE IF NOT EXISTS kg_relations (
    id               TEXT NOT NULL PRIMARY KEY,
    agent_id         TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id          VARCHAR(255) NOT NULL DEFAULT '',
    source_entity_id TEXT NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    relation_type    VARCHAR(200) NOT NULL,
    target_entity_id TEXT NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    confidence       REAL NOT NULL DEFAULT 1.0,
    properties       TEXT DEFAULT '{}',
    team_id          TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
    tenant_id        TEXT NOT NULL REFERENCES tenants(id),
    created_at       TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    valid_from  TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    valid_until TEXT,
    UNIQUE(agent_id, user_id, source_entity_id, relation_type, target_entity_id)
);

CREATE INDEX IF NOT EXISTS idx_kg_relations_source ON kg_relations(source_entity_id, relation_type);
CREATE INDEX IF NOT EXISTS idx_kg_relations_target ON kg_relations(target_entity_id);
CREATE INDEX IF NOT EXISTS idx_kg_relations_current ON kg_relations(agent_id, user_id) WHERE valid_until IS NULL;
CREATE INDEX IF NOT EXISTS idx_kg_relations_team ON kg_relations(team_id) WHERE team_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_kg_relations_tenant ON kg_relations(tenant_id);

-- ============================================================
-- Table: channel_pending_messages
-- ============================================================

CREATE TABLE IF NOT EXISTS channel_pending_messages (
    id              TEXT NOT NULL PRIMARY KEY,
    channel_name    VARCHAR(100) NOT NULL,
    history_key     VARCHAR(200) NOT NULL,
    sender          VARCHAR(255) NOT NULL,
    sender_id       VARCHAR(255) NOT NULL DEFAULT '',
    body            TEXT NOT NULL,
    platform_msg_id VARCHAR(100) NOT NULL DEFAULT '',
    is_summary      BOOLEAN NOT NULL DEFAULT 0,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_channel_pending_messages_lookup ON channel_pending_messages(channel_name, history_key, created_at);
CREATE INDEX IF NOT EXISTS idx_channel_pending_messages_tenant ON channel_pending_messages(tenant_id);

-- ============================================================
-- Table: channel_contacts
-- ============================================================

CREATE TABLE IF NOT EXISTS channel_contacts (
    id               TEXT NOT NULL PRIMARY KEY,
    channel_type     VARCHAR(50) NOT NULL,
    channel_instance VARCHAR(255),
    sender_id        VARCHAR(255) NOT NULL,
    user_id          VARCHAR(255),
    display_name     VARCHAR(255),
    username         VARCHAR(255),
    avatar_url       TEXT,
    peer_kind        VARCHAR(20),
    contact_type     VARCHAR(20) NOT NULL DEFAULT 'user',
    thread_id        VARCHAR(100),
    thread_type      VARCHAR(20),
    metadata         TEXT DEFAULT '{}',
    merged_id        TEXT,
    tenant_id        TEXT NOT NULL REFERENCES tenants(id),
    first_seen_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_seen_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- tenant-scoped unique including thread_id for topic contacts (migration 35)
CREATE UNIQUE INDEX IF NOT EXISTS idx_channel_contacts_tenant_type_sender ON channel_contacts(tenant_id, channel_type, sender_id, COALESCE(thread_id, ''));
CREATE INDEX IF NOT EXISTS idx_channel_contacts_instance ON channel_contacts(channel_instance) WHERE channel_instance IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_channel_contacts_merged ON channel_contacts(merged_id) WHERE merged_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_channel_contacts_search ON channel_contacts(display_name, username);
CREATE INDEX IF NOT EXISTS idx_channel_contacts_tenant ON channel_contacts(tenant_id);

-- ============================================================
-- Table: activity_logs
-- ============================================================

CREATE TABLE IF NOT EXISTS activity_logs (
    id          TEXT NOT NULL PRIMARY KEY,
    actor_type  VARCHAR(20) NOT NULL,
    actor_id    VARCHAR(255) NOT NULL,
    action      VARCHAR(100) NOT NULL,
    entity_type VARCHAR(50),
    entity_id   VARCHAR(255),
    details     TEXT,
    ip_address  VARCHAR(45),
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_activity_logs_actor ON activity_logs(actor_type, actor_id);
CREATE INDEX IF NOT EXISTS idx_activity_logs_action ON activity_logs(action);
CREATE INDEX IF NOT EXISTS idx_activity_logs_entity ON activity_logs(entity_type, entity_id);
CREATE INDEX IF NOT EXISTS idx_activity_logs_created ON activity_logs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_activity_logs_tenant ON activity_logs(tenant_id);

-- ============================================================
-- Table: usage_snapshots
-- ============================================================

CREATE TABLE IF NOT EXISTS usage_snapshots (
    id                  TEXT NOT NULL PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
    bucket_hour         TEXT NOT NULL,
    agent_id            TEXT,
    provider            VARCHAR(50) NOT NULL DEFAULT '',
    model               VARCHAR(200) NOT NULL DEFAULT '',
    channel             VARCHAR(50) NOT NULL DEFAULT '',
    input_tokens        BIGINT NOT NULL DEFAULT 0,
    output_tokens       BIGINT NOT NULL DEFAULT 0,
    cache_read_tokens   BIGINT NOT NULL DEFAULT 0,
    cache_create_tokens BIGINT NOT NULL DEFAULT 0,
    thinking_tokens     BIGINT NOT NULL DEFAULT 0,
    total_cost          NUMERIC(12,6) NOT NULL DEFAULT 0,
    request_count       INTEGER NOT NULL DEFAULT 0,
    llm_call_count      INTEGER NOT NULL DEFAULT 0,
    tool_call_count     INTEGER NOT NULL DEFAULT 0,
    error_count         INTEGER NOT NULL DEFAULT 0,
    unique_users        INTEGER NOT NULL DEFAULT 0,
    avg_duration_ms     INTEGER NOT NULL DEFAULT 0,
    memory_docs         INTEGER NOT NULL DEFAULT 0,
    memory_chunks       INTEGER NOT NULL DEFAULT 0,
    kg_entities         INTEGER NOT NULL DEFAULT 0,
    kg_relations        INTEGER NOT NULL DEFAULT 0,
    tenant_id           TEXT NOT NULL REFERENCES tenants(id),
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_usage_snapshots_bucket ON usage_snapshots(bucket_hour DESC);
CREATE INDEX IF NOT EXISTS idx_usage_snapshots_agent_bucket ON usage_snapshots(agent_id, bucket_hour DESC);
CREATE INDEX IF NOT EXISTS idx_usage_snapshots_provider_bucket ON usage_snapshots(provider, bucket_hour DESC) WHERE provider != '';
CREATE INDEX IF NOT EXISTS idx_usage_snapshots_channel_bucket ON usage_snapshots(channel, bucket_hour DESC) WHERE channel != '';
-- COALESCE NULLs to sentinel so upsert dedup works (SQLite treats NULL != NULL in unique indexes).
CREATE UNIQUE INDEX IF NOT EXISTS idx_usage_snapshots_unique ON usage_snapshots(
    bucket_hour,
    COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'),
    COALESCE(provider, ''),
    COALESCE(model, ''),
    COALESCE(channel, ''),
    tenant_id
);
CREATE INDEX IF NOT EXISTS idx_usage_snapshots_tenant ON usage_snapshots(tenant_id);

-- ============================================================
-- Table: builtin_tools
-- ============================================================

CREATE TABLE IF NOT EXISTS builtin_tools (
    name         VARCHAR(100) NOT NULL PRIMARY KEY,
    display_name VARCHAR(255) NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    category     VARCHAR(50) NOT NULL DEFAULT 'general',
    enabled      BOOLEAN NOT NULL DEFAULT 1,
    settings     TEXT NOT NULL DEFAULT '{}',
    requires     TEXT DEFAULT '[]',
    metadata     TEXT DEFAULT '{}',
    created_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_builtin_tools_category ON builtin_tools(category);

-- ============================================================
-- Table: builtin_tool_tenant_configs
-- ============================================================

CREATE TABLE IF NOT EXISTS builtin_tool_tenant_configs (
    tool_name  VARCHAR(100) NOT NULL REFERENCES builtin_tools(name) ON DELETE CASCADE,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    enabled    BOOLEAN,
    settings   TEXT,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (tool_name, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_builtin_tool_tenant_configs_tenant ON builtin_tool_tenant_configs(tenant_id);

-- ============================================================
-- Table: secure_cli_binaries
-- ============================================================

CREATE TABLE IF NOT EXISTS secure_cli_binaries (
    id              TEXT NOT NULL PRIMARY KEY,
    binary_name     TEXT NOT NULL,
    binary_path     TEXT,
    description     TEXT NOT NULL DEFAULT '',
    encrypted_env   BLOB NOT NULL,
    deny_args       TEXT NOT NULL DEFAULT '[]',
    deny_verbose    TEXT NOT NULL DEFAULT '[]',
    timeout_seconds INTEGER NOT NULL DEFAULT 30,
    tips            TEXT NOT NULL DEFAULT '',
    is_global       BOOLEAN NOT NULL DEFAULT 1,
    enabled         BOOLEAN NOT NULL DEFAULT 1,
    created_by      TEXT NOT NULL DEFAULT '',
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_secure_cli_binary_name ON secure_cli_binaries(binary_name);
CREATE UNIQUE INDEX IF NOT EXISTS idx_secure_cli_unique_binary_tenant ON secure_cli_binaries(binary_name, tenant_id);
CREATE INDEX IF NOT EXISTS idx_secure_cli_binaries_tenant ON secure_cli_binaries(tenant_id);

-- ============================================================
-- Table: secure_cli_agent_grants
-- ============================================================

CREATE TABLE IF NOT EXISTS secure_cli_agent_grants (
    id              TEXT NOT NULL PRIMARY KEY,
    binary_id       TEXT NOT NULL REFERENCES secure_cli_binaries(id) ON DELETE CASCADE,
    agent_id        TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    deny_args       TEXT,
    deny_verbose    TEXT,
    timeout_seconds INTEGER,
    tips            TEXT,
    enabled         BOOLEAN NOT NULL DEFAULT 1,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(binary_id, agent_id, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_scag_binary ON secure_cli_agent_grants(binary_id);
CREATE INDEX IF NOT EXISTS idx_scag_agent ON secure_cli_agent_grants(agent_id);
CREATE INDEX IF NOT EXISTS idx_scag_tenant ON secure_cli_agent_grants(tenant_id);

-- ============================================================
-- Table: api_keys
-- ============================================================

CREATE TABLE IF NOT EXISTS api_keys (
    id           TEXT NOT NULL PRIMARY KEY,
    name         VARCHAR(100) NOT NULL,
    prefix       VARCHAR(8) NOT NULL,
    key_hash     VARCHAR(64) NOT NULL UNIQUE,
    scopes       TEXT NOT NULL DEFAULT '[]',
    expires_at   TEXT,
    last_used_at TEXT,
    revoked      BOOLEAN NOT NULL DEFAULT 0,
    created_by   VARCHAR(255),
    owner_id     VARCHAR(255),
    tenant_id    TEXT REFERENCES tenants(id),
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash) WHERE NOT revoked;
CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys(prefix);
CREATE INDEX IF NOT EXISTS idx_api_keys_owner_id ON api_keys(owner_id) WHERE owner_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys(tenant_id) WHERE tenant_id IS NOT NULL;

-- ============================================================
-- Table: agent_heartbeats
-- ============================================================

CREATE TABLE IF NOT EXISTS agent_heartbeats (
    id                 TEXT NOT NULL PRIMARY KEY,
    agent_id           TEXT NOT NULL UNIQUE REFERENCES agents(id) ON DELETE CASCADE,
    enabled            BOOLEAN NOT NULL DEFAULT 0,
    interval_sec       INT NOT NULL DEFAULT 1800,
    prompt             TEXT,
    provider_id        TEXT REFERENCES llm_providers(id),
    model              VARCHAR(200),
    isolated_session   BOOLEAN NOT NULL DEFAULT 1,
    light_context      BOOLEAN NOT NULL DEFAULT 0,
    ack_max_chars      INT NOT NULL DEFAULT 300,
    max_retries        INT NOT NULL DEFAULT 2,
    active_hours_start VARCHAR(5),
    active_hours_end   VARCHAR(5),
    timezone           TEXT,
    channel            VARCHAR(50),
    chat_id            TEXT,
    next_run_at        TEXT,
    last_run_at        TEXT,
    last_status        VARCHAR(20),
    last_error         TEXT,
    run_count          INT NOT NULL DEFAULT 0,
    suppress_count     INT NOT NULL DEFAULT 0,
    metadata           TEXT DEFAULT '{}',
    created_at         TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at         TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_heartbeats_due ON agent_heartbeats(next_run_at) WHERE enabled = 1 AND next_run_at IS NOT NULL;

-- ============================================================
-- Table: heartbeat_run_logs
-- ============================================================

CREATE TABLE IF NOT EXISTS heartbeat_run_logs (
    id            TEXT NOT NULL PRIMARY KEY,
    heartbeat_id  TEXT NOT NULL REFERENCES agent_heartbeats(id) ON DELETE CASCADE,
    agent_id      TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    status        VARCHAR(20) NOT NULL,
    summary       TEXT,
    error         TEXT,
    duration_ms   INT,
    input_tokens  INT DEFAULT 0,
    output_tokens INT DEFAULT 0,
    skip_reason   VARCHAR(50),
    metadata      TEXT DEFAULT '{}',
    ran_at        TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    created_at    TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_hb_logs_heartbeat ON heartbeat_run_logs(heartbeat_id, ran_at DESC);
CREATE INDEX IF NOT EXISTS idx_hb_logs_agent ON heartbeat_run_logs(agent_id, ran_at DESC);

-- ============================================================
-- Table: agent_config_permissions
-- (scope widened to VARCHAR(255) in migration 23;
--  includes migrated group_file_writers rows)
-- ============================================================

CREATE TABLE IF NOT EXISTS agent_config_permissions (
    id          TEXT NOT NULL PRIMARY KEY,
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    scope       VARCHAR(255) NOT NULL,
    config_type VARCHAR(50) NOT NULL,
    user_id     VARCHAR(255) NOT NULL,
    permission  VARCHAR(10) NOT NULL,
    granted_by  VARCHAR(255),
    metadata    TEXT DEFAULT '{}',
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    created_at  TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at  TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(agent_id, scope, config_type, user_id)
);

CREATE INDEX IF NOT EXISTS idx_acp_lookup ON agent_config_permissions(agent_id, scope, config_type);
CREATE INDEX IF NOT EXISTS idx_agent_config_permissions_tenant ON agent_config_permissions(tenant_id);

-- ============================================================
-- Table: skill_tenant_configs (already defined above)
-- Table: builtin_tool_tenant_configs (already defined above)
-- ============================================================

-- ============================================================
-- Table: system_configs
-- ============================================================

CREATE TABLE IF NOT EXISTS system_configs (
    key        VARCHAR(100) NOT NULL,
    value      TEXT NOT NULL,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (key, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_system_configs_tenant ON system_configs(tenant_id);

-- ============================================================
-- Table: subagent_tasks
-- ============================================================

CREATE TABLE IF NOT EXISTS subagent_tasks (
    id                TEXT PRIMARY KEY,
    tenant_id         TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    parent_agent_key  VARCHAR(255) NOT NULL,
    session_key       VARCHAR(500),
    subject           VARCHAR(255) NOT NULL,
    description       TEXT NOT NULL,
    status            VARCHAR(20) NOT NULL DEFAULT 'running',
    result            TEXT,
    depth             INTEGER NOT NULL DEFAULT 1,
    model             VARCHAR(255),
    provider          VARCHAR(255),
    iterations        INTEGER NOT NULL DEFAULT 0,
    input_tokens      INTEGER NOT NULL DEFAULT 0,
    output_tokens     INTEGER NOT NULL DEFAULT 0,
    origin_channel    VARCHAR(50),
    origin_chat_id    VARCHAR(255),
    origin_peer_kind  VARCHAR(20),
    origin_user_id    VARCHAR(255),
    spawned_by        TEXT,
    completed_at      TEXT,
    archived_at       TEXT,
    metadata          TEXT NOT NULL DEFAULT '{}',
    custom_scope      TEXT,
    created_at        TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at        TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_subagent_tasks_parent_status ON subagent_tasks(tenant_id, parent_agent_key, status);
CREATE INDEX IF NOT EXISTS idx_subagent_tasks_session ON subagent_tasks(session_key);
CREATE INDEX IF NOT EXISTS idx_subagent_tasks_created ON subagent_tasks(tenant_id, created_at);

-- ============================================================
-- Table: episodic_summaries (V3 Tier 2 memory)
-- ============================================================

CREATE TABLE IF NOT EXISTS episodic_summaries (
    id          TEXT NOT NULL PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    session_key TEXT NOT NULL,
    summary     TEXT NOT NULL,
    l0_abstract TEXT NOT NULL DEFAULT '',
    key_topics  TEXT NOT NULL DEFAULT '[]',
    source_type TEXT NOT NULL DEFAULT 'session',
    source_id   TEXT,
    turn_count  INTEGER NOT NULL DEFAULT 0,
    token_count INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    expires_at  TEXT,
    promoted_at TEXT,
    -- Phase 10: dreaming weighted scoring signals (running avg of memory_search
    -- hit scores). See internal/consolidation/scoring.go::ComputeRecallScore.
    recall_count     INTEGER NOT NULL DEFAULT 0,
    recall_score     REAL    NOT NULL DEFAULT 0,
    last_recalled_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_episodic_agent_user ON episodic_summaries(agent_id, user_id);
CREATE INDEX IF NOT EXISTS idx_episodic_tenant ON episodic_summaries(tenant_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_episodic_source_dedup ON episodic_summaries(agent_id, user_id, source_id)
    WHERE source_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_episodic_unpromoted ON episodic_summaries(agent_id, user_id, created_at)
    WHERE promoted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_episodic_recall_unpromoted ON episodic_summaries(agent_id, user_id, recall_score DESC)
    WHERE promoted_at IS NULL;

-- ============================================================
-- Table: agent_evolution_metrics (V3 self-evolution Stage 1)
-- ============================================================

CREATE TABLE IF NOT EXISTS agent_evolution_metrics (
    id          TEXT NOT NULL PRIMARY KEY,
    tenant_id   TEXT NOT NULL REFERENCES tenants(id),
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    session_key TEXT NOT NULL,
    metric_type TEXT NOT NULL,
    metric_key  TEXT NOT NULL,
    value       TEXT NOT NULL,
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_evo_metrics_agent_type ON agent_evolution_metrics(agent_id, metric_type);
CREATE INDEX IF NOT EXISTS idx_evo_metrics_created ON agent_evolution_metrics(created_at);
CREATE INDEX IF NOT EXISTS idx_evo_metrics_tenant ON agent_evolution_metrics(tenant_id);

-- ============================================================
-- Table: agent_evolution_suggestions (V3 self-evolution Stage 2)
-- ============================================================

CREATE TABLE IF NOT EXISTS agent_evolution_suggestions (
    id              TEXT NOT NULL PRIMARY KEY,
    tenant_id       TEXT NOT NULL REFERENCES tenants(id),
    agent_id        TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    suggestion_type TEXT NOT NULL,
    suggestion      TEXT NOT NULL,
    rationale       TEXT NOT NULL,
    parameters      TEXT,
    status          TEXT NOT NULL DEFAULT 'pending',
    reviewed_by     TEXT,
    reviewed_at     TEXT,
    created_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE INDEX IF NOT EXISTS idx_evo_suggestions_agent ON agent_evolution_suggestions(agent_id, status);
CREATE INDEX IF NOT EXISTS idx_evo_suggestions_tenant ON agent_evolution_suggestions(tenant_id);

-- ============================================================
-- Table: kg_dedup_candidates (V3 dedup review queue)
-- ============================================================

CREATE TABLE IF NOT EXISTS kg_dedup_candidates (
    id          TEXT NOT NULL PRIMARY KEY,
    tenant_id   TEXT REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id    TEXT NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id     VARCHAR(255) NOT NULL DEFAULT '',
    entity_a_id TEXT NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    entity_b_id TEXT NOT NULL REFERENCES kg_entities(id) ON DELETE CASCADE,
    similarity  REAL NOT NULL,
    status      VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(entity_a_id, entity_b_id)
);

CREATE INDEX IF NOT EXISTS idx_kg_dedup_agent ON kg_dedup_candidates(agent_id, status);

-- ============================================================
-- Table: secure_cli_user_credentials (per-user encrypted env)
-- ============================================================

CREATE TABLE IF NOT EXISTS secure_cli_user_credentials (
    id            TEXT NOT NULL PRIMARY KEY,
    binary_id     TEXT NOT NULL REFERENCES secure_cli_binaries(id) ON DELETE CASCADE,
    user_id       VARCHAR(255) NOT NULL,
    encrypted_env BLOB NOT NULL,
    metadata      TEXT NOT NULL DEFAULT '{}',
    tenant_id     TEXT NOT NULL REFERENCES tenants(id),
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(binary_id, user_id, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_scuc_tenant ON secure_cli_user_credentials(tenant_id);
CREATE INDEX IF NOT EXISTS idx_scuc_binary ON secure_cli_user_credentials(binary_id);

-- ============================================================
-- Table: vault_documents (V3 Knowledge Vault registry)
-- ============================================================

CREATE TABLE IF NOT EXISTS vault_documents (
    id           TEXT NOT NULL PRIMARY KEY,
    tenant_id    TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id     TEXT REFERENCES agents(id) ON DELETE SET NULL,
    team_id      TEXT REFERENCES agent_teams(id) ON DELETE SET NULL,
    scope        TEXT NOT NULL DEFAULT 'personal',
    custom_scope TEXT,
    path         TEXT NOT NULL,
    title        TEXT NOT NULL DEFAULT '',
    doc_type     TEXT NOT NULL DEFAULT 'note',
    content_hash TEXT NOT NULL DEFAULT '',
    summary      TEXT NOT NULL DEFAULT '',
    metadata     TEXT DEFAULT '{}',
    created_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at   TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
-- SQLite prohibits expressions in inline UNIQUE constraints; use a unique index instead.
CREATE UNIQUE INDEX IF NOT EXISTS idx_vault_docs_unique_path
    ON vault_documents(tenant_id, COALESCE(agent_id, ''), COALESCE(team_id, ''), scope, path);
CREATE INDEX IF NOT EXISTS idx_vault_docs_tenant ON vault_documents(tenant_id);
CREATE INDEX IF NOT EXISTS idx_vault_docs_agent_scope ON vault_documents(agent_id, scope);
CREATE INDEX IF NOT EXISTS idx_vault_docs_type ON vault_documents(agent_id, doc_type);
CREATE INDEX IF NOT EXISTS idx_vault_docs_hash ON vault_documents(content_hash);
CREATE INDEX IF NOT EXISTS idx_vault_docs_team ON vault_documents(team_id);

-- ============================================================
-- Table: vault_links (V3 wikilink edges)
-- ============================================================

CREATE TABLE IF NOT EXISTS vault_links (
    id          TEXT NOT NULL PRIMARY KEY,
    from_doc_id TEXT NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    to_doc_id   TEXT NOT NULL REFERENCES vault_documents(id) ON DELETE CASCADE,
    link_type   TEXT NOT NULL DEFAULT 'wikilink',
    context     TEXT NOT NULL DEFAULT '',
    custom_scope TEXT,
    created_at  TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE(from_doc_id, to_doc_id, link_type)
);

CREATE INDEX IF NOT EXISTS idx_vault_links_from ON vault_links(from_doc_id);
CREATE INDEX IF NOT EXISTS idx_vault_links_to ON vault_links(to_doc_id);

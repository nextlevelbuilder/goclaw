-- Plan 2: Tenant Foundation
-- Master tenant UUID v7: 0193a5b0-7000-7000-8000-000000000001

-- ============================================================
-- Phase A: Create tenants + tenant_users tables
-- ============================================================

CREATE TABLE tenants (
    id         UUID PRIMARY KEY,
    name       VARCHAR(255) NOT NULL,
    slug       VARCHAR(100) NOT NULL UNIQUE,
    status     VARCHAR(20) NOT NULL DEFAULT 'active',
    settings   JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_tenants_slug ON tenants(slug);
CREATE INDEX idx_tenants_status ON tenants(status) WHERE status = 'active';

-- Seed master tenant
INSERT INTO tenants (id, name, slug, status)
VALUES ('0193a5b0-7000-7000-8000-000000000001', 'Master', 'master', 'active');

CREATE TABLE tenant_users (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id      VARCHAR(255) NOT NULL,
    display_name VARCHAR(255),
    role         VARCHAR(20) NOT NULL DEFAULT 'member',
    metadata     JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(tenant_id, user_id)
);

CREATE INDEX idx_tenant_users_user ON tenant_users(user_id);
CREATE INDEX idx_tenant_users_tenant ON tenant_users(tenant_id);

-- ============================================================
-- Phase B: ALTER ADD tenant_id to 30 tables
-- All default to master tenant UUID (PG 11+ metadata-only op)
-- ============================================================

-- Core tables
ALTER TABLE agents ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE sessions ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
-- api_keys: NULLABLE (NULL = system-level cross-tenant key)
ALTER TABLE api_keys ADD COLUMN tenant_id UUID REFERENCES tenants(id);

-- Agent ecosystem
ALTER TABLE agent_shares ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE user_context_files ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE user_agent_profiles ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE user_agent_overrides ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE agent_config_permissions ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE agent_links ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE channel_instances ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Memory + KG
ALTER TABLE memory_documents ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE memory_chunks ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE kg_entities ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE kg_relations ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Skills
ALTER TABLE skills ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE skill_user_grants ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Cron
ALTER TABLE cron_jobs ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Tracing + Activity
ALTER TABLE traces ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE activity_logs ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE usage_snapshots ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- MCP
ALTER TABLE mcp_servers ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE mcp_user_grants ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE mcp_access_requests ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Teams
ALTER TABLE agent_teams ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE team_user_grants ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Pairing + Channels
ALTER TABLE pairing_requests ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE paired_devices ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE channel_pending_messages ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE channel_contacts ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- LLM Providers + Config Secrets
ALTER TABLE llm_providers ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE config_secrets ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Other
ALTER TABLE secure_cli_binaries ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Grant tables
ALTER TABLE agent_context_files ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE skill_agent_grants ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE mcp_agent_grants ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Tasks + Tracing
ALTER TABLE team_tasks ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE spans ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Cache
ALTER TABLE embedding_cache ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- Team activity tables
ALTER TABLE agent_team_members ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE team_task_comments ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE team_task_events ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);
ALTER TABLE team_task_attachments ADD COLUMN tenant_id UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001' REFERENCES tenants(id);

-- ============================================================
-- Phase C: Drop defaults (force explicit tenant_id for new rows)
-- ============================================================

ALTER TABLE agents ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE sessions ALTER COLUMN tenant_id DROP DEFAULT;
-- api_keys: no default to drop (nullable, no DEFAULT set)
ALTER TABLE agent_shares ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE user_context_files ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE user_agent_profiles ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE user_agent_overrides ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE agent_config_permissions ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE agent_links ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE channel_instances ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE memory_documents ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE memory_chunks ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE kg_entities ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE kg_relations ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE skills ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE skill_user_grants ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE cron_jobs ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE traces ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE activity_logs ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE usage_snapshots ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE mcp_servers ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE mcp_user_grants ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE mcp_access_requests ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE agent_teams ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE team_user_grants ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE pairing_requests ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE paired_devices ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE channel_pending_messages ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE channel_contacts ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE llm_providers ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE config_secrets ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE secure_cli_binaries ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE agent_context_files ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE skill_agent_grants ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE mcp_agent_grants ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE team_tasks ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE spans ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE embedding_cache ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE agent_team_members ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE team_task_comments ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE team_task_events ALTER COLUMN tenant_id DROP DEFAULT;
ALTER TABLE team_task_attachments ALTER COLUMN tenant_id DROP DEFAULT;

-- ============================================================
-- Phase D: Indexes
-- ============================================================

-- Per-table tenant indexes
CREATE INDEX idx_agents_tenant ON agents(tenant_id);
CREATE INDEX idx_sessions_tenant ON sessions(tenant_id);
CREATE INDEX idx_api_keys_tenant ON api_keys(tenant_id) WHERE tenant_id IS NOT NULL;
CREATE INDEX idx_agent_shares_tenant ON agent_shares(tenant_id);
CREATE INDEX idx_user_context_files_tenant ON user_context_files(tenant_id);
CREATE INDEX idx_user_agent_profiles_tenant ON user_agent_profiles(tenant_id);
CREATE INDEX idx_user_agent_overrides_tenant ON user_agent_overrides(tenant_id);
CREATE INDEX idx_agent_config_permissions_tenant ON agent_config_permissions(tenant_id);
CREATE INDEX idx_agent_links_tenant ON agent_links(tenant_id);
CREATE INDEX idx_channel_instances_tenant ON channel_instances(tenant_id);
CREATE INDEX idx_memory_documents_tenant ON memory_documents(tenant_id);
CREATE INDEX idx_memory_chunks_tenant ON memory_chunks(tenant_id);
CREATE INDEX idx_kg_entities_tenant ON kg_entities(tenant_id);
CREATE INDEX idx_kg_relations_tenant ON kg_relations(tenant_id);
CREATE INDEX idx_skills_tenant ON skills(tenant_id);
CREATE INDEX idx_skill_user_grants_tenant ON skill_user_grants(tenant_id);
CREATE INDEX idx_cron_jobs_tenant ON cron_jobs(tenant_id);
CREATE INDEX idx_traces_tenant ON traces(tenant_id);
CREATE INDEX idx_activity_logs_tenant ON activity_logs(tenant_id);
CREATE INDEX idx_usage_snapshots_tenant ON usage_snapshots(tenant_id);
CREATE INDEX idx_mcp_servers_tenant ON mcp_servers(tenant_id);
CREATE INDEX idx_mcp_user_grants_tenant ON mcp_user_grants(tenant_id);
CREATE INDEX idx_mcp_access_requests_tenant ON mcp_access_requests(tenant_id);
CREATE INDEX idx_agent_teams_tenant ON agent_teams(tenant_id);
CREATE INDEX idx_team_user_grants_tenant ON team_user_grants(tenant_id);
CREATE INDEX idx_pairing_requests_tenant ON pairing_requests(tenant_id);
CREATE INDEX idx_paired_devices_tenant ON paired_devices(tenant_id);
CREATE INDEX idx_channel_pending_messages_tenant ON channel_pending_messages(tenant_id);
CREATE INDEX idx_channel_contacts_tenant ON channel_contacts(tenant_id);
CREATE INDEX idx_llm_providers_tenant ON llm_providers(tenant_id);
CREATE INDEX idx_config_secrets_tenant ON config_secrets(tenant_id);
CREATE INDEX idx_secure_cli_binaries_tenant ON secure_cli_binaries(tenant_id);
CREATE INDEX idx_agent_context_files_tenant ON agent_context_files(tenant_id);
CREATE INDEX idx_skill_agent_grants_tenant ON skill_agent_grants(tenant_id);
CREATE INDEX idx_mcp_agent_grants_tenant ON mcp_agent_grants(tenant_id);
CREATE INDEX idx_team_tasks_tenant ON team_tasks(tenant_id);
CREATE INDEX idx_spans_tenant ON spans(tenant_id);
CREATE INDEX idx_embedding_cache_tenant ON embedding_cache(tenant_id);
CREATE INDEX idx_agent_team_members_tenant ON agent_team_members(tenant_id);
CREATE INDEX idx_team_task_comments_tenant ON team_task_comments(tenant_id);
CREATE INDEX idx_team_task_events_tenant ON team_task_events(tenant_id);
CREATE INDEX idx_team_task_attachments_tenant ON team_task_attachments(tenant_id);

-- Composite indexes for Plan 3 query performance
CREATE INDEX idx_agents_tenant_active ON agents(tenant_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_sessions_tenant_user ON sessions(tenant_id, user_id);
CREATE INDEX idx_traces_tenant_time ON traces(tenant_id, created_at DESC);

-- ============================================================
-- Phase E: Seed master tenant owner from existing agents
-- ============================================================

INSERT INTO tenant_users (tenant_id, user_id, role)
SELECT DISTINCT '0193a5b0-7000-7000-8000-000000000001'::uuid, owner_id, 'owner'
FROM agents
WHERE owner_id IS NOT NULL AND owner_id != ''
LIMIT 1
ON CONFLICT (tenant_id, user_id) DO NOTHING;

-- ============================================================
-- Phase F: DROP custom_tools table (dead code — agent loop never wired)
-- ============================================================

DROP TABLE IF EXISTS custom_tools;

-- ============================================================
-- Phase G: Per-tenant builtin tool config overrides
-- ============================================================

CREATE TABLE builtin_tool_tenant_configs (
    tool_name  VARCHAR(100) NOT NULL REFERENCES builtin_tools(name) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    enabled    BOOLEAN,
    settings   JSONB,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tool_name, tenant_id)
);

CREATE INDEX idx_builtin_tool_tenant_configs_tenant ON builtin_tool_tenant_configs(tenant_id);

-- ============================================================
-- Phase H: Per-tenant skill config (disable system skills)
-- ============================================================

CREATE TABLE skill_tenant_configs (
    skill_id   UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    enabled    BOOLEAN NOT NULL DEFAULT true,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (skill_id, tenant_id)
);

CREATE INDEX idx_skill_tenant_configs_tenant ON skill_tenant_configs(tenant_id);

-- ============================================================
-- Phase J: MCP per-user credentials
-- ============================================================

CREATE TABLE mcp_user_credentials (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id  UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    api_key    TEXT,
    headers    BYTEA,
    env        BYTEA,
    tenant_id  UUID NOT NULL REFERENCES tenants(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(server_id, user_id, tenant_id)
);

CREATE INDEX idx_mcp_user_credentials_tenant ON mcp_user_credentials(tenant_id);
CREATE INDEX idx_mcp_user_credentials_server ON mcp_user_credentials(server_id);

-- ============================================================
-- Phase I: Update UNIQUE constraints to include tenant_id
-- Allows same name/key/slug across different tenants.
-- ============================================================

-- agents.agent_key: (agent_key) → (tenant_id, agent_key)
-- Old constraint replaced by partial index in migration 23.
DROP INDEX IF EXISTS idx_agents_agent_key_active;
CREATE UNIQUE INDEX idx_agents_tenant_agent_key_active ON agents(tenant_id, agent_key) WHERE deleted_at IS NULL;

-- sessions.session_key: globally unique → (tenant_id, session_key)
ALTER TABLE sessions DROP CONSTRAINT IF EXISTS sessions_session_key_key;
DROP INDEX IF EXISTS sessions_session_key_key;
CREATE UNIQUE INDEX idx_sessions_tenant_session_key ON sessions(tenant_id, session_key);

-- skills.slug: globally unique → (tenant_id, slug)
ALTER TABLE skills DROP CONSTRAINT IF EXISTS skills_slug_key;
DROP INDEX IF EXISTS skills_slug_key;
CREATE UNIQUE INDEX idx_skills_tenant_slug ON skills(tenant_id, slug);

-- mcp_servers.name: globally unique → (tenant_id, name)
ALTER TABLE mcp_servers DROP CONSTRAINT IF EXISTS mcp_servers_name_key;
DROP INDEX IF EXISTS mcp_servers_name_key;
CREATE UNIQUE INDEX idx_mcp_servers_tenant_name ON mcp_servers(tenant_id, name);

-- channel_contacts: (channel_type, sender_id) → (tenant_id, channel_type, sender_id)
ALTER TABLE channel_contacts DROP CONSTRAINT IF EXISTS channel_contacts_channel_type_sender_id_key;
DROP INDEX IF EXISTS channel_contacts_channel_type_sender_id_key;
CREATE UNIQUE INDEX idx_channel_contacts_tenant_type_sender ON channel_contacts(tenant_id, channel_type, sender_id);

-- llm_providers.name: globally unique → (tenant_id, name)
ALTER TABLE llm_providers DROP CONSTRAINT IF EXISTS llm_providers_name_key;
DROP INDEX IF EXISTS llm_providers_name_key;
CREATE UNIQUE INDEX idx_llm_providers_tenant_name ON llm_providers(tenant_id, name);

-- config_secrets.key: PK (key) → PK (key, tenant_id)
ALTER TABLE config_secrets DROP CONSTRAINT IF EXISTS config_secrets_pkey;
ALTER TABLE config_secrets ADD PRIMARY KEY (key, tenant_id);

-- paired_devices: UNIQUE (sender_id, channel) → (tenant_id, sender_id, channel)
ALTER TABLE paired_devices DROP CONSTRAINT IF EXISTS paired_devices_sender_id_channel_key;
DROP INDEX IF EXISTS paired_devices_sender_id_channel_key;
CREATE UNIQUE INDEX idx_paired_devices_tenant_sender_channel ON paired_devices(tenant_id, sender_id, channel);

-- channel_instances.name: globally unique → (tenant_id, name)
ALTER TABLE channel_instances DROP CONSTRAINT IF EXISTS channel_instances_name_key;
DROP INDEX IF EXISTS channel_instances_name_key;
CREATE UNIQUE INDEX idx_channel_instances_tenant_name ON channel_instances(tenant_id, name);

-- usage_snapshots: add tenant_id to unique conflict index for per-tenant aggregation
DROP INDEX IF EXISTS idx_usage_snapshots_unique;
CREATE UNIQUE INDEX idx_usage_snapshots_unique ON usage_snapshots (
    bucket_hour,
    COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'::uuid),
    provider, model, channel,
    tenant_id
);

-- Cleanup: strip leaked gateway tokens from session media URLs.
-- Old code embedded ?token=GATEWAY_TOKEN in markdown image URLs stored in session messages.
-- New code stores clean paths; frontend adds auth at render time.
UPDATE sessions
SET messages = regexp_replace(messages::text, '\?token=[a-f0-9]+', '', 'g')::jsonb
WHERE messages::text LIKE '%?token=%';

-- ============================================================
-- Phase K: Migrate remaining UUID v4 defaults to v7
-- ============================================================

ALTER TABLE kg_entities          ALTER COLUMN id SET DEFAULT uuid_generate_v7();
ALTER TABLE kg_relations         ALTER COLUMN id SET DEFAULT uuid_generate_v7();
ALTER TABLE channel_contacts     ALTER COLUMN id SET DEFAULT uuid_generate_v7();
ALTER TABLE team_user_grants     ALTER COLUMN id SET DEFAULT uuid_generate_v7();
ALTER TABLE tenant_users         ALTER COLUMN id SET DEFAULT uuid_generate_v7();
ALTER TABLE mcp_user_credentials ALTER COLUMN id SET DEFAULT uuid_generate_v7();

-- Phase 1: add created_by to llm_providers (it has neither created_by nor updated_by)
ALTER TABLE llm_providers
    ADD COLUMN IF NOT EXISTS created_by VARCHAR(255),
    ADD COLUMN IF NOT EXISTS updated_by VARCHAR(255);

-- Phase 2: add updated_by to tables that already have created_by but lack updated_by
ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS updated_by VARCHAR(255);

ALTER TABLE skills
    ADD COLUMN IF NOT EXISTS updated_by VARCHAR(255);

ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS updated_by VARCHAR(255);

ALTER TABLE cron_jobs
    ADD COLUMN IF NOT EXISTS updated_by VARCHAR(255);

ALTER TABLE custom_tools
    ADD COLUMN IF NOT EXISTS updated_by VARCHAR(255);

ALTER TABLE channel_instances
    ADD COLUMN IF NOT EXISTS updated_by VARCHAR(255);

ALTER TABLE mcp_servers
    ADD COLUMN IF NOT EXISTS updated_by VARCHAR(255);

-- Indexes for filter performance (created_by / owner_id / user_id)
CREATE INDEX IF NOT EXISTS idx_llm_providers_created_by ON llm_providers(created_by);
CREATE INDEX IF NOT EXISTS idx_custom_tools_created_by  ON custom_tools(created_by);
CREATE INDEX IF NOT EXISTS idx_channel_instances_created_by ON channel_instances(created_by);
CREATE INDEX IF NOT EXISTS idx_mcp_servers_created_by   ON mcp_servers(created_by);
-- agents/skills already have owner_id indexes; sessions/cron_jobs already have user_id indexes

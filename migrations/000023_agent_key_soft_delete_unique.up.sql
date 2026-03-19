-- Replace hard UNIQUE constraint on agent_key with a partial unique index
-- that excludes soft-deleted rows. This allows reusing an agent_key after
-- the original agent has been deleted (soft-deleted via deleted_at).
-- Fixes: https://github.com/nextlevelbuilder/goclaw/issues/180
-- Fixes: https://github.com/nextlevelbuilder/goclaw/issues/181

ALTER TABLE agents DROP CONSTRAINT agents_agent_key_key;
CREATE UNIQUE INDEX idx_agents_agent_key_active ON agents(agent_key) WHERE deleted_at IS NULL;

DROP INDEX IF EXISTS idx_agents_agent_key_active;
ALTER TABLE agents ADD CONSTRAINT agents_agent_key_key UNIQUE (agent_key);

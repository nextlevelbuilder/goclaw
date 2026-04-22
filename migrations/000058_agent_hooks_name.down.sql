-- 000054 down — Reverse rename, re-add agent_id, drop junction + name.

ALTER TABLE hooks RENAME TO agent_hooks;
ALTER TABLE hook_agents RENAME TO agent_hook_agents;

-- Re-add deprecated column (nullable, no data to restore)
ALTER TABLE agent_hooks ADD COLUMN IF NOT EXISTS agent_id UUID REFERENCES agents(id) ON DELETE CASCADE;

-- Restore original indexes
DROP INDEX IF EXISTS idx_hooks_lookup;
CREATE INDEX idx_hooks_lookup ON agent_hooks (tenant_id, agent_id, event) WHERE enabled = TRUE;
DROP INDEX IF EXISTS idx_hook_agents_agent;
CREATE INDEX idx_hook_agents_agent ON agent_hook_agents (agent_id);

-- Drop junction table + name column
DROP TABLE IF EXISTS agent_hook_agents;
ALTER TABLE agent_hooks DROP COLUMN IF EXISTS name;

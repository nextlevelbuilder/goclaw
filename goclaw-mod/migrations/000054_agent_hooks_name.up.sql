-- 000054 — Add `name` column, N:M junction, rename tables, drop agent_id.
--
-- 1) name: user-facing label. Nullable so existing rows don't break.
-- 2) Junction table replaces the 1:N agent_id FK with many-to-many.
-- 3) Data migration: copy existing agent_id values into junction.
-- 4) Rename agent_hooks → hooks, agent_hook_agents → hook_agents.
-- 5) Drop deprecated agent_id column from hooks.
-- 6) Recreate indexes with new table names.

-- Step 1: add name column
ALTER TABLE agent_hooks ADD COLUMN IF NOT EXISTS name VARCHAR(255);

-- Step 2: create junction table
CREATE TABLE IF NOT EXISTS agent_hook_agents (
    hook_id  UUID NOT NULL REFERENCES agent_hooks(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    PRIMARY KEY (hook_id, agent_id)
);

CREATE INDEX IF NOT EXISTS idx_hook_agents_agent
    ON agent_hook_agents (agent_id);

-- Step 3: copy existing agent_id values into junction
INSERT INTO agent_hook_agents (hook_id, agent_id)
SELECT id, agent_id FROM agent_hooks
WHERE agent_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- Step 4: rename tables
ALTER TABLE agent_hooks RENAME TO hooks;
ALTER TABLE agent_hook_agents RENAME TO hook_agents;

-- Step 5: drop deprecated column
ALTER TABLE hooks DROP COLUMN IF EXISTS agent_id;

-- Step 6: recreate indexes with new table/column names
DROP INDEX IF EXISTS idx_hooks_lookup;
CREATE INDEX idx_hooks_lookup ON hooks (tenant_id, event) WHERE enabled = TRUE;

DROP INDEX IF EXISTS idx_hook_agents_agent;
CREATE INDEX idx_hook_agents_agent ON hook_agents (agent_id);

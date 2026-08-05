DROP INDEX IF EXISTS idx_agents_tenant_visibility;
ALTER TABLE agents DROP COLUMN IF EXISTS visibility;

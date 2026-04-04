-- Reverse: add agent_id back, drop is_global, drop grants table
ALTER TABLE secure_cli_binaries ADD COLUMN agent_id UUID REFERENCES agents(id) ON DELETE CASCADE;

DROP INDEX IF EXISTS idx_secure_cli_unique_binary_tenant;
CREATE UNIQUE INDEX idx_secure_cli_unique_binary_agent
    ON secure_cli_binaries(binary_name, COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'::uuid));
CREATE INDEX idx_secure_cli_agent_id ON secure_cli_binaries(agent_id) WHERE agent_id IS NOT NULL;

ALTER TABLE secure_cli_binaries DROP COLUMN IF EXISTS is_global;
DROP TABLE IF EXISTS secure_cli_agent_grants;

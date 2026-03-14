ALTER TABLE llm_providers     DROP COLUMN IF EXISTS created_by, DROP COLUMN IF EXISTS updated_by;
ALTER TABLE agents            DROP COLUMN IF EXISTS updated_by;
ALTER TABLE skills            DROP COLUMN IF EXISTS updated_by;
ALTER TABLE sessions          DROP COLUMN IF EXISTS updated_by;
ALTER TABLE cron_jobs         DROP COLUMN IF EXISTS updated_by;
ALTER TABLE custom_tools      DROP COLUMN IF EXISTS updated_by;
ALTER TABLE channel_instances DROP COLUMN IF EXISTS updated_by;
ALTER TABLE mcp_servers       DROP COLUMN IF EXISTS updated_by;

DROP INDEX IF EXISTS idx_llm_providers_created_by;
DROP INDEX IF EXISTS idx_custom_tools_created_by;
DROP INDEX IF EXISTS idx_channel_instances_created_by;
DROP INDEX IF EXISTS idx_mcp_servers_created_by;

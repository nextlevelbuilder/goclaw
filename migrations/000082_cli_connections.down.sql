-- Reverse 000082. Safe because the migration was expand-only: it never dropped
-- agents.connected_agents or connected_agent_credentials, so rolling back returns
-- to the per-agent model with no data loss. Credentials saved AFTER the migration
-- (against the new tenant-level rows) are lost on rollback — they have no place in
-- the old (agent_id, connection_id) shape.

BEGIN;

DROP TABLE IF EXISTS cli_connection_credentials;

DROP INDEX IF EXISTS idx_cli_connections_tenant_enabled;
DROP INDEX IF EXISTS idx_cli_connections_global_name;
DROP INDEX IF EXISTS idx_cli_connections_tenant_name;

DROP TABLE IF EXISTS cli_connections;

COMMIT;

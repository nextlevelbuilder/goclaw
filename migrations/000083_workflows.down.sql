-- Reverses 000083. Dropping the table discards authored graphs; the compiled
-- primitives (cron entries) are NOT cleaned up here because they live in their
-- own tables and a down-migration cannot know which of them a workflow created
-- without the `compiled` column it is about to drop. Retract by disabling
-- workflows BEFORE migrating down.

BEGIN;
DROP INDEX IF EXISTS workflows_enabled_idx;
DROP INDEX IF EXISTS workflows_tenant_updated_idx;
DROP INDEX IF EXISTS workflows_tenant_name_idx;
DROP TABLE IF EXISTS workflows;
COMMIT;

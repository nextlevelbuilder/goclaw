-- 000053 down — restore the pre-script constraints. Any rows with the new
-- values are deleted first so the restored CHECK won't reject valid historical
-- rows. Uniqueness indexes are recreated in their original form.

DELETE FROM agent_hooks WHERE handler_type = 'script' OR source = 'builtin';

ALTER TABLE agent_hooks DROP CONSTRAINT IF EXISTS agent_hooks_handler_type_check;
ALTER TABLE agent_hooks ADD CONSTRAINT agent_hooks_handler_type_check
  CHECK (handler_type IN ('command', 'http', 'prompt'));

ALTER TABLE agent_hooks DROP CONSTRAINT IF EXISTS agent_hooks_source_check;
ALTER TABLE agent_hooks ADD CONSTRAINT agent_hooks_source_check
  CHECK (source IN ('ui', 'api', 'seed'));
ALTER TABLE agent_hooks ALTER COLUMN source TYPE VARCHAR(8);

CREATE UNIQUE INDEX IF NOT EXISTS uq_hooks_global
  ON agent_hooks (event, handler_type) WHERE scope = 'global';
CREATE UNIQUE INDEX IF NOT EXISTS uq_hooks_tenant
  ON agent_hooks (tenant_id, event, handler_type) WHERE scope = 'tenant';
CREATE UNIQUE INDEX IF NOT EXISTS uq_hooks_agent
  ON agent_hooks (tenant_id, agent_id, event, handler_type) WHERE scope = 'agent';

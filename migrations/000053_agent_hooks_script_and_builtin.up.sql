-- 000053 — Relax CHECK constraints for script handler + builtin source.
--
-- Phase 03 of the Wave 1 hooks plan. Before this migration the hook system
-- only accepts command/http/prompt handlers and ui/api/seed sources. Phase 02
-- landed a goja-backed script handler; Phase 04 ships builtin hooks whose
-- source marker must be persisted as 'builtin'. Uniqueness indexes on
-- (event, handler_type) per scope are also dropped — scripts routinely want
-- many small hooks per event (e.g. a redactor per PII type).

ALTER TABLE agent_hooks DROP CONSTRAINT IF EXISTS agent_hooks_handler_type_check;
ALTER TABLE agent_hooks ADD CONSTRAINT agent_hooks_handler_type_check
  CHECK (handler_type IN ('command', 'http', 'prompt', 'script'));

ALTER TABLE agent_hooks ALTER COLUMN source TYPE VARCHAR(16);
ALTER TABLE agent_hooks DROP CONSTRAINT IF EXISTS agent_hooks_source_check;
ALTER TABLE agent_hooks ADD CONSTRAINT agent_hooks_source_check
  CHECK (source IN ('ui', 'api', 'seed', 'builtin'));

DROP INDEX IF EXISTS uq_hooks_global;
DROP INDEX IF EXISTS uq_hooks_tenant;
DROP INDEX IF EXISTS uq_hooks_agent;

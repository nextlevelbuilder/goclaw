-- Migration 000081 down: remove FK constraints added in up migration

BEGIN;

ALTER TABLE usage_events      DROP CONSTRAINT IF EXISTS usage_events_team_id_fkey;
ALTER TABLE traces            DROP CONSTRAINT IF EXISTS traces_agent_id_fkey;
ALTER TABLE spans             DROP CONSTRAINT IF EXISTS spans_agent_id_fkey;
ALTER TABLE spans             DROP CONSTRAINT IF EXISTS spans_trace_id_fkey;
ALTER TABLE webhook_calls     DROP CONSTRAINT IF EXISTS webhook_calls_agent_id_fkey;
ALTER TABLE webhook_calls     DROP CONSTRAINT IF EXISTS webhook_calls_tenant_id_fkey;
ALTER TABLE webhooks          DROP CONSTRAINT IF EXISTS webhooks_tenant_id_fkey;
ALTER TABLE tenant_hook_budget DROP CONSTRAINT IF EXISTS tenant_hook_budget_tenant_id_fkey;
ALTER TABLE hooks             DROP CONSTRAINT IF EXISTS hooks_tenant_id_fkey;

COMMIT;

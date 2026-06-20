-- Migration 000081: add missing FK constraints for referential integrity
--
-- Audit found 9 missing FK constraints across 6 tables.
-- Orphan audit confirmed 0 orphaned rows in all cases.
--
-- Phase 1: clean orphans (all counts were 0, but DELETE guards are idempotent)
-- Phase 2: fix tenant_id drift (none found, guard included)
-- Phase 3: add FK constraints

BEGIN;

-- ── Phase 1: clean orphans ──────────────────────────────────────────────────
-- All orphan counts were 0 at time of migration authoring, but these DELETEs
-- are safe guards in case any slip in before the migration runs.

-- webhook_calls orphaned agent_id (nullable)
DELETE FROM webhook_calls
WHERE agent_id IS NOT NULL
  AND agent_id NOT IN (SELECT id FROM agents);

-- webhook_calls orphaned tenant_id (NOT NULL)
DELETE FROM webhook_calls
WHERE tenant_id NOT IN (SELECT id FROM tenants);

-- webhooks orphaned tenant_id (NOT NULL)
DELETE FROM webhooks
WHERE tenant_id NOT IN (SELECT id FROM tenants);

-- hooks orphaned tenant_id (NOT NULL)
-- Global-scope hooks use the sentinel MasterTenantID which must exist in tenants.
DELETE FROM hooks
WHERE tenant_id NOT IN (SELECT id FROM tenants);

-- tenant_hook_budget orphaned tenant_id (NOT NULL, PK)
DELETE FROM tenant_hook_budget
WHERE tenant_id NOT IN (SELECT id FROM tenants);

-- spans orphaned agent_id (nullable)
DELETE FROM spans
WHERE agent_id IS NOT NULL
  AND agent_id NOT IN (SELECT id FROM agents);

-- spans orphaned trace_id (NOT NULL)
DELETE FROM spans
WHERE trace_id NOT IN (SELECT id FROM traces);

-- traces orphaned agent_id (nullable)
DELETE FROM traces
WHERE agent_id IS NOT NULL
  AND agent_id NOT IN (SELECT id FROM agents);

-- usage_events orphaned team_id (nullable)
DELETE FROM usage_events
WHERE team_id IS NOT NULL
  AND team_id NOT IN (SELECT id FROM agent_teams);

-- ── Phase 3: add FK constraints ─────────────────────────────────────────────

-- hooks.tenant_id → tenants(id)
ALTER TABLE hooks
    ADD CONSTRAINT hooks_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;

-- tenant_hook_budget.tenant_id → tenants(id)
ALTER TABLE tenant_hook_budget
    ADD CONSTRAINT tenant_hook_budget_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;

-- webhooks.tenant_id → tenants(id)
ALTER TABLE webhooks
    ADD CONSTRAINT webhooks_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;

-- webhook_calls.tenant_id → tenants(id)
ALTER TABLE webhook_calls
    ADD CONSTRAINT webhook_calls_tenant_id_fkey
    FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE CASCADE;

-- webhook_calls.agent_id → agents(id) (nullable, SET NULL on agent delete)
ALTER TABLE webhook_calls
    ADD CONSTRAINT webhook_calls_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE SET NULL;

-- spans.trace_id → traces(id) (NOT NULL, cascade)
ALTER TABLE spans
    ADD CONSTRAINT spans_trace_id_fkey
    FOREIGN KEY (trace_id) REFERENCES traces(id) ON DELETE CASCADE;

-- spans.agent_id → agents(id) (nullable, SET NULL on agent delete)
ALTER TABLE spans
    ADD CONSTRAINT spans_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE SET NULL;

-- traces.agent_id → agents(id) (nullable, SET NULL on agent delete)
ALTER TABLE traces
    ADD CONSTRAINT traces_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE SET NULL;

-- usage_events.team_id → agent_teams(id) (nullable, SET NULL on team delete)
ALTER TABLE usage_events
    ADD CONSTRAINT usage_events_team_id_fkey
    FOREIGN KEY (team_id) REFERENCES agent_teams(id) ON DELETE SET NULL;

COMMIT;

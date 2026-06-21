-- Migration 082: FK constraints round 2 + hook_executions tenant isolation + indexes
-- Zero orphans verified before each constraint addition.

-- ---------------------------------------------------------------------------
-- 1. webhooks.channel_id → channel_instances(id) ON DELETE SET NULL
-- ---------------------------------------------------------------------------
ALTER TABLE webhooks
    ADD CONSTRAINT fk_webhooks_channel_id
    FOREIGN KEY (channel_id) REFERENCES channel_instances(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_webhooks_channel_id ON webhooks(channel_id);

-- ---------------------------------------------------------------------------
-- 2. channel_contacts.merged_id → channel_contacts(id) self-ref ON DELETE SET NULL
-- ---------------------------------------------------------------------------
ALTER TABLE channel_contacts
    ADD CONSTRAINT fk_channel_contacts_merged_id
    FOREIGN KEY (merged_id) REFERENCES channel_contacts(id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- 3. traces.parent_trace_id → traces(id) self-ref ON DELETE SET NULL
-- ---------------------------------------------------------------------------
ALTER TABLE traces
    ADD CONSTRAINT fk_traces_parent_trace_id
    FOREIGN KEY (parent_trace_id) REFERENCES traces(id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- 4. spans.parent_span_id → spans(id) self-ref ON DELETE SET NULL
-- ---------------------------------------------------------------------------
ALTER TABLE spans
    ADD CONSTRAINT fk_spans_parent_span_id
    FOREIGN KEY (parent_span_id) REFERENCES spans(id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- 5. subagent_tasks.spawned_by → subagent_tasks(id) self-ref ON DELETE SET NULL
-- ---------------------------------------------------------------------------
ALTER TABLE subagent_tasks
    ADD CONSTRAINT fk_subagent_tasks_spawned_by
    FOREIGN KEY (spawned_by) REFERENCES subagent_tasks(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_subagent_tasks_spawned_by ON subagent_tasks(spawned_by);

-- ---------------------------------------------------------------------------
-- 6. team_task_attachments.created_by_agent_id: RESTRICT → SET NULL
--    (all other created_by_agent_id columns use SET NULL; make this consistent)
-- ---------------------------------------------------------------------------
ALTER TABLE team_task_attachments
    DROP CONSTRAINT team_task_attachments_created_by_agent_id_fkey;

ALTER TABLE team_task_attachments
    ADD CONSTRAINT fk_team_task_attachments_created_by_agent_id
    FOREIGN KEY (created_by_agent_id) REFERENCES agents(id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- 7. hook_executions: add tenant_id for tenant isolation
--    hook_id is nullable (ON DELETE SET NULL), so backfill only where hook_id IS NOT NULL.
--    Rows with NULL hook_id will have NULL tenant_id — column is nullable by design.
-- ---------------------------------------------------------------------------
ALTER TABLE hook_executions
    ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id) ON DELETE CASCADE;

UPDATE hook_executions he
    SET tenant_id = h.tenant_id
    FROM hooks h
    WHERE he.hook_id = h.id
      AND he.hook_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_hook_executions_hook_id   ON hook_executions(hook_id);
CREATE INDEX IF NOT EXISTS idx_hook_executions_tenant_id ON hook_executions(tenant_id);

-- ---------------------------------------------------------------------------
-- 8. Missing indexes on FK-candidate columns
-- ---------------------------------------------------------------------------
-- hooks.created_by: no FK (OAuth UUID, no referencing table), but benefits from index
CREATE INDEX IF NOT EXISTS idx_hooks_created_by ON hooks(created_by);

-- usage_events.team_id: FK added in 081 but index was missing
CREATE INDEX IF NOT EXISTS idx_usage_events_team_id ON usage_events(team_id);

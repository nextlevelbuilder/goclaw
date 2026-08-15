-- Rollback migration 082

-- 8. Remove extra indexes
DROP INDEX IF EXISTS idx_usage_events_team_id;
DROP INDEX IF EXISTS idx_hooks_created_by;

-- 7. hook_executions tenant isolation
DROP INDEX IF EXISTS idx_hook_executions_tenant_id;
DROP INDEX IF EXISTS idx_hook_executions_hook_id;
ALTER TABLE hook_executions DROP COLUMN IF EXISTS tenant_id;

-- 6. team_task_attachments: revert to original constraint name + RESTRICT (NO ACTION)
ALTER TABLE team_task_attachments
    DROP CONSTRAINT IF EXISTS fk_team_task_attachments_created_by_agent_id;

ALTER TABLE team_task_attachments
    ADD CONSTRAINT team_task_attachments_created_by_agent_id_fkey
    FOREIGN KEY (created_by_agent_id) REFERENCES agents(id);

-- 5. subagent_tasks self-ref
DROP INDEX IF EXISTS idx_subagent_tasks_spawned_by;
ALTER TABLE subagent_tasks DROP CONSTRAINT IF EXISTS fk_subagent_tasks_spawned_by;

-- 4. spans self-ref
ALTER TABLE spans DROP CONSTRAINT IF EXISTS fk_spans_parent_span_id;

-- 3. traces self-ref
ALTER TABLE traces DROP CONSTRAINT IF EXISTS fk_traces_parent_trace_id;

-- 2. channel_contacts self-ref
ALTER TABLE channel_contacts DROP CONSTRAINT IF EXISTS fk_channel_contacts_merged_id;

-- 1. webhooks.channel_id
DROP INDEX IF EXISTS idx_webhooks_channel_id;
ALTER TABLE webhooks DROP CONSTRAINT IF EXISTS fk_webhooks_channel_id;

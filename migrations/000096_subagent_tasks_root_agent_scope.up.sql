-- Persist immutable root-agent ownership separately from the human-readable key.
-- Existing rows prefer the UUID captured in metadata. Rows without that metadata
-- are backfilled by key only when exactly one matching agent predates the task.
-- This prevents a newly recreated same-key agent from inheriting older tasks.
-- Ambiguous or unmatched rows remain NULL and are intentionally inaccessible.
ALTER TABLE subagent_tasks
    ADD COLUMN root_agent_id UUID;

WITH metadata_owners AS (
    SELECT task.id AS task_id, agent.id AS root_agent_id
    FROM subagent_tasks AS task
    JOIN agents AS agent
      ON agent.tenant_id = task.tenant_id
     AND agent.id::text = task.metadata->>'root_agent_id'
    WHERE task.metadata ? 'root_agent_id'
)
UPDATE subagent_tasks AS task
SET root_agent_id = owner.root_agent_id
FROM metadata_owners AS owner
WHERE task.id = owner.task_id;

WITH unique_key_owners AS (
    SELECT task.id AS task_id, agent.id AS root_agent_id
    FROM subagent_tasks AS task
    JOIN agents AS agent
      ON agent.tenant_id = task.tenant_id
     AND agent.agent_key = task.parent_agent_key
     AND agent.created_at < task.created_at
    WHERE task.root_agent_id IS NULL
      AND NOT (task.metadata ? 'root_agent_id')
      AND NOT EXISTS (
          SELECT 1
          FROM agents AS other
          WHERE other.tenant_id = agent.tenant_id
            AND other.agent_key = agent.agent_key
            AND other.created_at < task.created_at
            AND other.id <> agent.id
      )
)
UPDATE subagent_tasks AS task
SET root_agent_id = owner.root_agent_id
FROM unique_key_owners AS owner
WHERE task.id = owner.task_id;

ALTER TABLE subagent_tasks
    ADD CONSTRAINT fk_subagent_tasks_root_agent
    FOREIGN KEY (root_agent_id, tenant_id)
    REFERENCES agents(id, tenant_id)
    ON DELETE SET NULL (root_agent_id);

CREATE INDEX idx_subagent_tasks_root_status
    ON subagent_tasks(tenant_id, root_agent_id, status, created_at DESC)
    WHERE root_agent_id IS NOT NULL;

CREATE INDEX idx_subagent_tasks_root_session
    ON subagent_tasks(tenant_id, root_agent_id, session_key, created_at DESC)
    WHERE root_agent_id IS NOT NULL AND session_key IS NOT NULL;

CREATE INDEX idx_subagent_tasks_root_archive
    ON subagent_tasks(tenant_id, root_agent_id, completed_at, id)
    WHERE root_agent_id IS NOT NULL
      AND status IN ('completed', 'failed', 'cancelled')
      AND archived_at IS NULL;

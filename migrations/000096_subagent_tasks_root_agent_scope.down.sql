DROP INDEX IF EXISTS idx_subagent_tasks_root_archive;
DROP INDEX IF EXISTS idx_subagent_tasks_root_session;
DROP INDEX IF EXISTS idx_subagent_tasks_root_status;

ALTER TABLE subagent_tasks
    DROP CONSTRAINT IF EXISTS fk_subagent_tasks_root_agent;

ALTER TABLE subagent_tasks
    DROP COLUMN IF EXISTS root_agent_id;

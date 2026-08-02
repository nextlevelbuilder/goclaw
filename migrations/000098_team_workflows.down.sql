DROP INDEX IF EXISTS idx_team_tasks_dispatch_recovery;
DROP INDEX IF EXISTS idx_team_tasks_workflow_status;
DROP INDEX IF EXISTS idx_team_tasks_workflow_step;

ALTER TABLE team_tasks
    DROP CONSTRAINT IF EXISTS team_tasks_workflow_fields_check,
    DROP CONSTRAINT IF EXISTS team_tasks_workflow_kind_check,
    DROP COLUMN IF EXISTS dispatch_lease_until,
    DROP COLUMN IF EXISTS dispatch_token,
    DROP COLUMN IF EXISTS workflow_terminal,
    DROP COLUMN IF EXISTS workflow_kind,
    DROP COLUMN IF EXISTS workflow_step_id,
    DROP COLUMN IF EXISTS workflow_id;

DROP TABLE IF EXISTS team_workflows;

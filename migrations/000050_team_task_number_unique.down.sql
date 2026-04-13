-- Revert: drop unique constraint on (team_id, task_number).
ALTER TABLE team_tasks
    DROP CONSTRAINT IF EXISTS uq_team_tasks_team_number;

ALTER TABLE team_tasks
    DROP CONSTRAINT IF EXISTS team_tasks_notification_policy_check,
    DROP COLUMN IF EXISTS notification_policy;

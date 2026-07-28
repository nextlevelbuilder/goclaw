ALTER TABLE team_tasks
    ADD COLUMN notification_policy VARCHAR(24) NOT NULL DEFAULT 'default';

UPDATE team_tasks
SET notification_policy = 'workflow_internal'
WHERE workflow_id IS NOT NULL
  AND workflow_kind IN ('audit', 'work');

ALTER TABLE team_tasks
    ADD CONSTRAINT team_tasks_notification_policy_check
        CHECK (notification_policy IN ('default', 'suppress_handoff', 'workflow_internal'));

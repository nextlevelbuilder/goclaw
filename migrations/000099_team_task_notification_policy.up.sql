-- Typed user-notification policy with workflow backfill.
--
-- IDEMPOTENT BY DESIGN (see 000097 header). A VPS that ran the pre-renumber
-- notification step at version 97 already carries this column + check; a plain
-- ADD COLUMN would fail and wedge the migration.
ALTER TABLE team_tasks
    ADD COLUMN IF NOT EXISTS notification_policy VARCHAR(24) NOT NULL DEFAULT 'default';

-- Idempotent: re-running sets the same value on the same rows.
UPDATE team_tasks
SET notification_policy = 'workflow_internal'
WHERE workflow_id IS NOT NULL
  AND workflow_kind IN ('audit', 'work');

ALTER TABLE team_tasks
    DROP CONSTRAINT IF EXISTS team_tasks_notification_policy_check;
ALTER TABLE team_tasks
    ADD CONSTRAINT team_tasks_notification_policy_check
        CHECK (notification_policy IN ('default', 'suppress_handoff', 'workflow_internal'));

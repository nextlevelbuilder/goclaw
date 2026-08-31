-- Reverse of 000099_team_work_enforcement.up.sql.
--
-- NOTE: the subagent root-agent scope converged in section 7 of the up migration
-- is NOT reversed here — it belongs to upstream 000096_subagent_tasks_root_agent_scope
-- and must survive a Team Work rollback so the subagent system keeps functioning.
--
-- Refuse rollback while any v98-only durable state is populated. The predecessor
-- has nowhere to represent these values, so dropping them would silently lose
-- operator-visible workflow history. dispatch_count is the one exception: it is
-- translated back into metadata below.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM team_workflows
        WHERE status NOT IN ('pending_expansion', 'running', 'failing', 'completed', 'failed')
           OR delivery_status NOT IN ('pending', 'enqueuing', 'delivered')
           OR plan_revision IS DISTINCT FROM 1
           OR expansion_attempt_count IS DISTINCT FROM 0
           OR next_expansion_at IS NOT NULL
           OR last_expansion_error IS DISTINCT FROM ''
           OR delivery_attempt_count IS DISTINCT FROM 0
           OR next_delivery_at IS NOT NULL
           OR last_delivery_error IS DISTINCT FROM ''
           OR cancel_reason IS DISTINCT FROM ''
           OR cancelled_at IS NOT NULL
           OR classification_audit_id IS NOT NULL
    ) THEN
        RAISE EXCEPTION 'cannot roll back team work enforcement: v99-only workflow state exists';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM team_tasks
        WHERE plan_revision IS DISTINCT FROM 1
           OR blocker_reason IS DISTINCT FROM ''
           OR recovery_count IS DISTINCT FROM 0
           OR escalation_status IS DISTINCT FROM 'pending'
           OR escalation_attempt_count IS DISTINCT FROM 0
           OR escalation_next_at IS NOT NULL
           OR escalation_last_error IS DISTINCT FROM ''
    ) THEN
        RAISE EXCEPTION 'cannot roll back team work enforcement: v99-only task state exists';
    END IF;

    IF EXISTS (SELECT 1 FROM team_work_classification_audits) THEN
        RAISE EXCEPTION 'cannot roll back team work enforcement: classifier audit rows exist';
    END IF;
END
$$;

ALTER TABLE team_workflows
    DROP CONSTRAINT IF EXISTS team_workflows_classification_audit_fk;

DROP INDEX IF EXISTS idx_twc_audits_tenant_run;
DROP INDEX IF EXISTS idx_twc_audits_tenant_session_time;
DROP INDEX IF EXISTS idx_twc_audits_tenant_time;
DROP TABLE IF EXISTS team_work_classification_audits;

DROP INDEX IF EXISTS idx_team_tasks_active_owner;

-- Restore the non-revision-aware unique step key.
DROP INDEX IF EXISTS idx_team_tasks_workflow_step;
CREATE UNIQUE INDEX idx_team_tasks_workflow_step
    ON team_tasks(tenant_id, workflow_id, workflow_step_id)
    WHERE workflow_id IS NOT NULL;

-- Move dispatch_count back into metadata before dropping the column.
UPDATE team_tasks
SET metadata = jsonb_set(COALESCE(metadata, '{}'::jsonb), '{dispatch_count}', to_jsonb(dispatch_count), true)
WHERE dispatch_count <> 0
   OR metadata ? 'dispatch_count';

ALTER TABLE team_tasks
    DROP CONSTRAINT IF EXISTS team_tasks_escalation_status_check,
    DROP COLUMN IF EXISTS escalation_last_error,
    DROP COLUMN IF EXISTS escalation_next_at,
    DROP COLUMN IF EXISTS escalation_attempt_count,
    DROP COLUMN IF EXISTS escalation_status,
    DROP COLUMN IF EXISTS recovery_count,
    DROP COLUMN IF EXISTS blocker_reason,
    DROP COLUMN IF EXISTS dispatch_count,
    DROP COLUMN IF EXISTS plan_revision;

ALTER TABLE team_workflows
    DROP COLUMN IF EXISTS classification_audit_id,
    DROP COLUMN IF EXISTS cancelled_at,
    DROP COLUMN IF EXISTS cancel_reason,
    DROP COLUMN IF EXISTS last_delivery_error,
    DROP COLUMN IF EXISTS next_delivery_at,
    DROP COLUMN IF EXISTS delivery_attempt_count,
    DROP COLUMN IF EXISTS last_expansion_error,
    DROP COLUMN IF EXISTS next_expansion_at,
    DROP COLUMN IF EXISTS expansion_attempt_count,
    DROP COLUMN IF EXISTS plan_revision;

ALTER TABLE team_workflows
    DROP CONSTRAINT team_workflows_delivery_status_check;
ALTER TABLE team_workflows
    ADD CONSTRAINT team_workflows_delivery_status_check
        CHECK (delivery_status IN ('pending', 'enqueuing', 'delivered'));

ALTER TABLE team_workflows
    DROP CONSTRAINT team_workflows_status_check;
ALTER TABLE team_workflows
    ADD CONSTRAINT team_workflows_status_check CHECK (
        status IN ('pending_expansion', 'running', 'failing', 'completed', 'failed')
    );

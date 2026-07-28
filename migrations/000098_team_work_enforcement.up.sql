-- Team Work enforcement deltas: attempt fencing, blocker/recovery, bounded
-- expansion/delivery, plan revisions, owner exclusion and classification audit.
-- Authored fresh on top of 000096_team_workflows and 000097_team_task_notification_policy.

-- ---------------------------------------------------------------------------
-- 1. Expanded workflow lifecycle statuses.
-- ---------------------------------------------------------------------------
ALTER TABLE team_workflows
    DROP CONSTRAINT team_workflows_status_check;
ALTER TABLE team_workflows
    ADD CONSTRAINT team_workflows_status_check CHECK (
        status IN (
            'pending_expansion',
            'running',
            'needs_revision',
            'failing',
            'cancelling',
            'completed',
            'failed',
            'cancelled'
        )
    );

-- Bounded external delivery adds a terminal dead/manual state.
ALTER TABLE team_workflows
    DROP CONSTRAINT team_workflows_delivery_status_check;
ALTER TABLE team_workflows
    ADD CONSTRAINT team_workflows_delivery_status_check
        CHECK (delivery_status IN ('pending', 'enqueuing', 'delivered', 'dead'));

-- ---------------------------------------------------------------------------
-- 2. Workflow revision + bounded expansion / delivery / cancellation state.
-- ---------------------------------------------------------------------------
ALTER TABLE team_workflows
    ADD COLUMN plan_revision           INT NOT NULL DEFAULT 1,
    ADD COLUMN expansion_attempt_count INT NOT NULL DEFAULT 0,
    ADD COLUMN next_expansion_at       TIMESTAMPTZ,
    ADD COLUMN last_expansion_error    TEXT NOT NULL DEFAULT '',
    ADD COLUMN delivery_attempt_count  INT NOT NULL DEFAULT 0,
    ADD COLUMN next_delivery_at        TIMESTAMPTZ,
    ADD COLUMN last_delivery_error     TEXT NOT NULL DEFAULT '',
    ADD COLUMN cancel_reason           TEXT NOT NULL DEFAULT '',
    ADD COLUMN cancelled_at            TIMESTAMPTZ,
    ADD COLUMN classification_audit_id UUID;

-- ---------------------------------------------------------------------------
-- 3. Workflow task revision, durable dispatch count, blocker/recovery and
--    coordinator-escalation retry state.
-- ---------------------------------------------------------------------------
ALTER TABLE team_tasks
    ADD COLUMN plan_revision            INT NOT NULL DEFAULT 1,
    ADD COLUMN dispatch_count           INT NOT NULL DEFAULT 0,
    ADD COLUMN blocker_reason           TEXT NOT NULL DEFAULT '',
    ADD COLUMN recovery_count           INT NOT NULL DEFAULT 0,
    ADD COLUMN escalation_status        VARCHAR(16) NOT NULL DEFAULT 'pending',
    ADD COLUMN escalation_attempt_count INT NOT NULL DEFAULT 0,
    ADD COLUMN escalation_next_at       TIMESTAMPTZ,
    ADD COLUMN escalation_last_error    TEXT NOT NULL DEFAULT '';

ALTER TABLE team_tasks
    ADD CONSTRAINT team_tasks_escalation_status_check
        CHECK (escalation_status IN ('pending', 'enqueuing', 'delivered', 'dead'));

-- Migrate the legacy dispatch_count that lived in the metadata JSON blob into
-- the durable column, then drop it from metadata so there is a single source
-- of truth.
UPDATE team_tasks
SET dispatch_count = COALESCE((metadata->>'dispatch_count')::int, 0)
WHERE metadata ? 'dispatch_count';
UPDATE team_tasks
SET metadata = metadata - 'dispatch_count'
WHERE metadata ? 'dispatch_count';

-- ---------------------------------------------------------------------------
-- 4. Revision-aware unique step key.
-- ---------------------------------------------------------------------------
DROP INDEX idx_team_tasks_workflow_step;
CREATE UNIQUE INDEX idx_team_tasks_workflow_step
    ON team_tasks(tenant_id, workflow_id, plan_revision, workflow_step_id)
    WHERE workflow_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 5. Owner exclusion: at most one active workflow work task per owner/tenant.
-- Existing duplicates are an operator-visible invariant violation. Abort rather
-- than silently choosing a winner or rewriting live task ownership.
-- ---------------------------------------------------------------------------
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM team_tasks
        WHERE workflow_kind = 'work'
          AND owner_agent_id IS NOT NULL
          AND status IN ('dispatching', 'in_progress')
        GROUP BY tenant_id, owner_agent_id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'cannot enforce active workflow owner uniqueness: duplicate active owners exist';
    END IF;
END
$$;

CREATE UNIQUE INDEX idx_team_tasks_active_owner
    ON team_tasks(tenant_id, owner_agent_id)
    WHERE workflow_kind = 'work'
      AND owner_agent_id IS NOT NULL
      AND status IN ('dispatching', 'in_progress');

-- ---------------------------------------------------------------------------
-- 6. Append-only classifier audit.
-- ---------------------------------------------------------------------------
CREATE TABLE team_work_classification_audits (
    id                     UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id              UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    ingress                VARCHAR(16) NOT NULL,
    run_id                 VARCHAR(255) NOT NULL DEFAULT '',
    session_key            VARCHAR(500) NOT NULL DEFAULT '',
    agent_id               UUID REFERENCES agents(id) ON DELETE SET NULL,
    original_hash          VARCHAR(64) NOT NULL DEFAULT '',
    resolved_hash          VARCHAR(64) NOT NULL DEFAULT '',
    verified_shape         VARCHAR(40) NOT NULL DEFAULT '',
    traits                 JSONB NOT NULL DEFAULT '[]'::jsonb,
    requested_mode         VARCHAR(24) NOT NULL DEFAULT '',
    effective_mode         VARCHAR(24) NOT NULL DEFAULT '',
    independent_review     BOOLEAN NOT NULL DEFAULT FALSE,
    selected_owner_agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    coordinator_agent_id   UUID REFERENCES agents(id) ON DELETE SET NULL,
    plan_hash              VARCHAR(64) NOT NULL DEFAULT '',
    stage_statuses         JSONB NOT NULL DEFAULT '{}'::jsonb,
    degraded_stage         VARCHAR(40) NOT NULL DEFAULT '',
    degraded_reason        TEXT NOT NULL DEFAULT '',
    classifier_provider    VARCHAR(60) NOT NULL DEFAULT '',
    classifier_model       VARCHAR(120) NOT NULL DEFAULT '',
    schema_version         INT NOT NULL DEFAULT 1,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT team_work_classification_audits_ingress_check
        CHECK (ingress IN ('inbound', 'ws', 'system')),
    CONSTRAINT team_work_classification_audits_requested_mode_check
        CHECK (requested_mode IN ('', 'self', 'single_owner', 'multi_role')),
    CONSTRAINT team_work_classification_audits_effective_mode_check
        CHECK (effective_mode IN ('', 'self', 'single_owner', 'multi_role'))
);

CREATE INDEX idx_twc_audits_tenant_time
    ON team_work_classification_audits(tenant_id, created_at DESC);
CREATE INDEX idx_twc_audits_tenant_session_time
    ON team_work_classification_audits(tenant_id, session_key, created_at DESC);
CREATE INDEX idx_twc_audits_tenant_run
    ON team_work_classification_audits(tenant_id, run_id);

-- Late FK from workflows to the audit row (created after the table exists).
ALTER TABLE team_workflows
    ADD CONSTRAINT team_workflows_classification_audit_fk
        FOREIGN KEY (classification_audit_id)
        REFERENCES team_work_classification_audits(id) ON DELETE SET NULL;

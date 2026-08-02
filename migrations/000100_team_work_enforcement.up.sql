-- Team Work enforcement deltas: attempt fencing, blocker/recovery, bounded
-- expansion/delivery, plan revisions, owner exclusion and classification audit.
-- Authored fresh on top of 000097_team_workflows and 000098_team_task_notification_policy.
--
-- IDEMPOTENT BY DESIGN. golang-migrate derives the version from the filename and
-- `migrate up` applies every file whose version is greater than the current
-- schema_migrations value. The live VPS ran the PRE-renumber enforcement deltas
-- under version 98, so after this branch renumbers enforcement to 99 the VPS
-- arrives at `migrate up` with version = 98 and these exact objects ALREADY
-- PRESENT. Re-running non-idempotent DDL there would fail with "column/constraint
-- already exists", flip schema_migrations.dirty = true, and wedge production.
-- Every statement below is therefore guarded so it is a no-op when its object
-- already exists and correct on a fresh database.

-- ---------------------------------------------------------------------------
-- 1. Expanded workflow lifecycle statuses.
--    DROP ... IF EXISTS + re-ADD is idempotent: safe whether the old or the new
--    check is present, and converges to the widened set.
-- ---------------------------------------------------------------------------
ALTER TABLE team_workflows
    DROP CONSTRAINT IF EXISTS team_workflows_status_check;
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
    DROP CONSTRAINT IF EXISTS team_workflows_delivery_status_check;
ALTER TABLE team_workflows
    ADD CONSTRAINT team_workflows_delivery_status_check
        CHECK (delivery_status IN ('pending', 'enqueuing', 'delivered', 'dead'));

-- ---------------------------------------------------------------------------
-- 2. Workflow revision + bounded expansion / delivery / cancellation state.
--    ADD COLUMN IF NOT EXISTS: no-op on the VPS where these columns already
--    exist; added on a fresh database.
-- ---------------------------------------------------------------------------
ALTER TABLE team_workflows
    ADD COLUMN IF NOT EXISTS plan_revision           INT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS expansion_attempt_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS next_expansion_at       TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_expansion_error    TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS delivery_attempt_count  INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS next_delivery_at        TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_delivery_error     TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS cancel_reason           TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS cancelled_at            TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS classification_audit_id UUID;

-- ---------------------------------------------------------------------------
-- 3. Workflow task revision, durable dispatch count, blocker/recovery and
--    coordinator-escalation retry state.
-- ---------------------------------------------------------------------------
ALTER TABLE team_tasks
    ADD COLUMN IF NOT EXISTS plan_revision            INT NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS dispatch_count           INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS blocker_reason           TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS recovery_count           INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS escalation_status        VARCHAR(16) NOT NULL DEFAULT 'pending',
    ADD COLUMN IF NOT EXISTS escalation_attempt_count INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS escalation_next_at       TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS escalation_last_error    TEXT NOT NULL DEFAULT '';

-- No ADD CONSTRAINT IF NOT EXISTS in PostgreSQL, so DROP IF EXISTS + re-ADD.
ALTER TABLE team_tasks
    DROP CONSTRAINT IF EXISTS team_tasks_escalation_status_check;
ALTER TABLE team_tasks
    ADD CONSTRAINT team_tasks_escalation_status_check
        CHECK (escalation_status IN ('pending', 'enqueuing', 'delivered', 'dead'));

-- Migrate the legacy dispatch_count that lived in the metadata JSON blob into
-- the durable column, then drop it from metadata so there is a single source
-- of truth. Naturally idempotent: a second run finds no rows still carrying the
-- key and updates nothing.
UPDATE team_tasks
SET dispatch_count = COALESCE((metadata->>'dispatch_count')::int, 0)
WHERE metadata ? 'dispatch_count';
UPDATE team_tasks
SET metadata = metadata - 'dispatch_count'
WHERE metadata ? 'dispatch_count';

-- ---------------------------------------------------------------------------
-- 4. Revision-aware unique step key.
--    The pre-renumber migration created the old (tenant_id, workflow_id,
--    workflow_step_id) unique index under this same name; drop-if-exists +
--    create-if-not-exists converges both the fresh and the already-migrated DB
--    onto the revision-aware shape.
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_team_tasks_workflow_step;
CREATE UNIQUE INDEX IF NOT EXISTS idx_team_tasks_workflow_step
    ON team_tasks(tenant_id, workflow_id, plan_revision, workflow_step_id)
    WHERE workflow_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 5. Owner exclusion: at most one active workflow work task per owner/tenant.
-- Existing duplicates are an operator-visible invariant violation. Abort rather
-- than silently choosing a winner or rewriting live task ownership. This is a
-- data guard, not DDL, so it evaluates live data on every run; a clean database
-- passes it whether this is the first or a repeated application.
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

CREATE UNIQUE INDEX IF NOT EXISTS idx_team_tasks_active_owner
    ON team_tasks(tenant_id, owner_agent_id)
    WHERE workflow_kind = 'work'
      AND owner_agent_id IS NOT NULL
      AND status IN ('dispatching', 'in_progress');

-- ---------------------------------------------------------------------------
-- 6. Append-only classifier audit.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS team_work_classification_audits (
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

CREATE INDEX IF NOT EXISTS idx_twc_audits_tenant_time
    ON team_work_classification_audits(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_twc_audits_tenant_session_time
    ON team_work_classification_audits(tenant_id, session_key, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_twc_audits_tenant_run
    ON team_work_classification_audits(tenant_id, run_id);

-- Late FK from workflows to the audit row (created after the table exists).
-- No ADD CONSTRAINT IF NOT EXISTS, so DROP IF EXISTS + re-ADD for idempotency.
ALTER TABLE team_workflows
    DROP CONSTRAINT IF EXISTS team_workflows_classification_audit_fk;
ALTER TABLE team_workflows
    ADD CONSTRAINT team_workflows_classification_audit_fk
        FOREIGN KEY (classification_audit_id)
        REFERENCES team_work_classification_audits(id) ON DELETE SET NULL;

-- ---------------------------------------------------------------------------
-- 7. CONVERGENCE: subagent root-agent scope.
--
-- Upstream ships this schema under 000096_subagent_tasks_root_agent_scope, but
-- the live VPS ran the PRE-renumber Team Work chain at version 96, so on the VPS
-- schema_migrations.version is already >= 96 and `migrate up` SKIPS upstream's
-- 96 forever — the VPS would be permanently missing subagent_tasks.root_agent_id,
-- which the upstream subagent store (internal/store/pg/subagent_tasks.go) selects
-- and filters on, crashing at runtime. This is the ONLY migration guaranteed to
-- run on such a VPS (99 > every prior Team Work version), so the missing scope is
-- converged here. Every statement is idempotent: on a fresh database or one that
-- already ran upstream 000096 the ADD COLUMN IF NOT EXISTS is a no-op, the
-- backfills are guarded by root_agent_id IS NULL, and the FK + indexes use
-- DROP/ADD IF EXISTS + CREATE INDEX IF NOT EXISTS.
-- ---------------------------------------------------------------------------
ALTER TABLE subagent_tasks
    ADD COLUMN IF NOT EXISTS root_agent_id UUID;

-- Backfill from authoritative metadata; idempotent (only touches NULL rows).
WITH metadata_owners AS (
    SELECT task.id AS task_id, agent.id AS root_agent_id
    FROM subagent_tasks AS task
    JOIN agents AS agent
      ON agent.tenant_id = task.tenant_id
     AND agent.id::text = task.metadata->>'root_agent_id'
    WHERE task.metadata ? 'root_agent_id'
      AND task.root_agent_id IS NULL
)
UPDATE subagent_tasks AS task
SET root_agent_id = owner.root_agent_id
FROM metadata_owners AS owner
WHERE task.id = owner.task_id;

-- Key fallback only when exactly one matching agent predates the task.
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
    DROP CONSTRAINT IF EXISTS fk_subagent_tasks_root_agent;
ALTER TABLE subagent_tasks
    ADD CONSTRAINT fk_subagent_tasks_root_agent
        FOREIGN KEY (root_agent_id, tenant_id)
        REFERENCES agents(id, tenant_id)
        ON DELETE SET NULL (root_agent_id);

CREATE INDEX IF NOT EXISTS idx_subagent_tasks_root_status
    ON subagent_tasks(tenant_id, root_agent_id, status, created_at DESC)
    WHERE root_agent_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_subagent_tasks_root_session
    ON subagent_tasks(tenant_id, root_agent_id, session_key, created_at DESC)
    WHERE root_agent_id IS NOT NULL AND session_key IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_subagent_tasks_root_archive
    ON subagent_tasks(tenant_id, root_agent_id, completed_at, id)
    WHERE root_agent_id IS NOT NULL
      AND status IN ('completed', 'failed', 'cancelled')
      AND archived_at IS NULL;

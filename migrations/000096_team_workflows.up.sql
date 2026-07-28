CREATE TABLE team_workflows (
    id                      UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    team_id                 UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    tenant_id               UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    status                  VARCHAR(24) NOT NULL,
    canonical_plan          JSONB NOT NULL,
    schema_version          INT NOT NULL,
    plan_hash               VARCHAR(64) NOT NULL,
    coordinator_agent_id    UUID NOT NULL REFERENCES agents(id),
    coordinator_agent_key   VARCHAR(255) NOT NULL,
    origin_agent_id         UUID NOT NULL REFERENCES agents(id),
    origin_agent_key        VARCHAR(255) NOT NULL,
    origin_run_id           VARCHAR(255) NOT NULL,
    origin_session_key      VARCHAR(500) NOT NULL,
    origin_channel          VARCHAR(60) NOT NULL,
    origin_chat_id          VARCHAR(255) NOT NULL,
    origin_peer_kind        VARCHAR(20) NOT NULL DEFAULT 'direct',
    origin_local_key        VARCHAR(500) NOT NULL DEFAULT '',
    origin_user_id          VARCHAR(255) NOT NULL DEFAULT '',
    origin_sender_id        VARCHAR(255) NOT NULL DEFAULT '',
    origin_role             VARCHAR(60) NOT NULL DEFAULT '',
	 origin_routing          JSONB NOT NULL DEFAULT '{}'::jsonb,
    auto_expand             BOOLEAN NOT NULL DEFAULT FALSE,
    audit_task_id           UUID,
    terminal_task_id        UUID,
    expansion_token         UUID,
    expansion_lease_until   TIMESTAMPTZ,
    finalize_token          UUID,
    finalize_lease_until    TIMESTAMPTZ,
    finalize_claimed_at     TIMESTAMPTZ,
    finalized_at            TIMESTAMPTZ,
    failure_settle_deadline TIMESTAMPTZ,
    failure_summary         TEXT NOT NULL DEFAULT '',
    result_summary          TEXT NOT NULL DEFAULT '',
	 delivery_status         VARCHAR(16) NOT NULL DEFAULT 'pending',
	 delivery_token          UUID,
	 delivery_lease_until    TIMESTAMPTZ,
	 delivered_at            TIMESTAMPTZ,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT team_workflows_status_check CHECK (
        status IN ('pending_expansion', 'running', 'failing', 'completed', 'failed')
    ),
	CONSTRAINT team_workflows_plan_hash_check CHECK (plan_hash ~ '^[0-9a-f]{64}$'),
	CONSTRAINT team_workflows_delivery_status_check CHECK (delivery_status IN ('pending','enqueuing','delivered'))
);

CREATE UNIQUE INDEX idx_team_workflows_creation
    ON team_workflows(tenant_id, team_id, origin_run_id, plan_hash);
CREATE INDEX idx_team_workflows_plan_lookup
    ON team_workflows(tenant_id, team_id, plan_hash, status);
CREATE INDEX idx_team_workflows_recovery
    ON team_workflows(tenant_id, status, expansion_lease_until, finalize_lease_until);
CREATE INDEX idx_team_workflows_delivery_recovery
    ON team_workflows(tenant_id, delivery_status, delivery_lease_until)
    WHERE finalized_at IS NOT NULL AND delivered_at IS NULL;

ALTER TABLE team_tasks
    ADD COLUMN workflow_id UUID REFERENCES team_workflows(id) ON DELETE CASCADE,
    ADD COLUMN workflow_step_id VARCHAR(100),
    ADD COLUMN workflow_kind VARCHAR(10),
    ADD COLUMN workflow_terminal BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN dispatch_token UUID,
    ADD COLUMN dispatch_lease_until TIMESTAMPTZ;

ALTER TABLE team_workflows
    ADD CONSTRAINT team_workflows_audit_task_fk
        FOREIGN KEY (audit_task_id) REFERENCES team_tasks(id) ON DELETE SET NULL,
    ADD CONSTRAINT team_workflows_terminal_task_fk
        FOREIGN KEY (terminal_task_id) REFERENCES team_tasks(id) ON DELETE SET NULL;

ALTER TABLE team_tasks
    ADD CONSTRAINT team_tasks_workflow_kind_check
        CHECK (workflow_kind IS NULL OR workflow_kind IN ('audit', 'work')),
    ADD CONSTRAINT team_tasks_workflow_fields_check CHECK (
        (workflow_id IS NULL AND workflow_step_id IS NULL AND workflow_kind IS NULL
            AND workflow_terminal = FALSE AND dispatch_token IS NULL AND dispatch_lease_until IS NULL)
        OR
        (workflow_id IS NOT NULL AND workflow_kind = 'audit' AND workflow_step_id IS NULL
            AND workflow_terminal = FALSE AND dispatch_token IS NULL AND dispatch_lease_until IS NULL)
        OR
        (workflow_id IS NOT NULL AND workflow_kind = 'work' AND workflow_step_id IS NOT NULL)
    );

CREATE UNIQUE INDEX idx_team_tasks_workflow_step
    ON team_tasks(tenant_id, workflow_id, workflow_step_id)
    WHERE workflow_id IS NOT NULL;
CREATE INDEX idx_team_tasks_workflow_status
    ON team_tasks(tenant_id, workflow_id, status)
    WHERE workflow_id IS NOT NULL;
CREATE INDEX idx_team_tasks_dispatch_recovery
    ON team_tasks(tenant_id, status, dispatch_lease_until)
    WHERE workflow_id IS NOT NULL AND workflow_kind = 'work';

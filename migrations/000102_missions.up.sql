-- Durable mission data model (Mission Mode). A mission is a named objective
-- with goals, milestones, and acceptance criteria, tied to an owning agent and
-- a session. checkpoint_seq links the mission to the latest durable run
-- checkpoint snapshot (run_checkpoint_snapshots.seq) so a paused mission can be
-- resumed by the cron "mission" branch from where it left off.
CREATE TABLE missions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    goals         TEXT[] NOT NULL DEFAULT '{}',
    milestones    TEXT[] NOT NULL DEFAULT '{}',
    acceptance    TEXT[] NOT NULL DEFAULT '{}',
    status        VARCHAR(40) NOT NULL DEFAULT 'active',
    agent_id      UUID REFERENCES agents(id) ON DELETE SET NULL,
    session_key   TEXT NOT NULL DEFAULT '',
    checkpoint_seq INT NOT NULL DEFAULT 0,
    metadata      JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Tenant-scoped listing, newest first (mission.list default order).
CREATE INDEX idx_missions_tenant_created
    ON missions(tenant_id, created_at DESC);
-- Tenant-scoped status filter (mission.list filtered by status).
CREATE INDEX idx_missions_tenant_status
    ON missions(tenant_id, status);
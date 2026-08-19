-- Persistent command-execution approval queue. Rows are written best-effort by
-- the ExecApprovalManager when an agent's command requires human approval, and
-- updated to a terminal status when an operator approves/denies the request or
-- the in-memory timeout fires. payload carries opaque JSON (e.g. deny-group
-- matched, originating session); command is the exact shell text shown to the
-- approver. status: pending | approved | denied | expired.
CREATE TABLE approval_requests (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id        UUID REFERENCES agents(id) ON DELETE SET NULL,
    requester_id    UUID,
    requester_type  TEXT,
    action_type     TEXT NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}'::jsonb,
    command         TEXT,
    status          TEXT NOT NULL,
    decision        TEXT,
    decided_by      UUID,
    allow_once      BOOLEAN,
    allow_always    BOOLEAN,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    decided_at      TIMESTAMPTZ,
    expired_at      TIMESTAMPTZ,
    timeout_seconds INT NOT NULL DEFAULT 120
);

-- Tenant-scoped pending/history listing (approval queue + audit surface).
CREATE INDEX idx_approval_requests_tenant_status
    ON approval_requests(tenant_id, status);
-- Per-agent lookup when replaying/resuming an approval for a specific agent.
CREATE INDEX idx_approval_requests_agent
    ON approval_requests(agent_id);

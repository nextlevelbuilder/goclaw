-- Durable multi-agent collaboration records (handoff, jury, competition,
-- negotiation). One row per collaboration event; body stores the full
-- JSON-encoded contract plus any verdicts/counter-proposals produced during
-- the run. The store layer treats body as opaque (JSONB enforces valid JSON).
CREATE TABLE multi_agent_records (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    run_id     TEXT,
    kind       VARCHAR(40) NOT NULL,
    body       JSONB NOT NULL DEFAULT '{}'::jsonb,
    status     VARCHAR(40) NOT NULL DEFAULT 'draft',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Tenant-scoped listing is the primary read path (newest first).
CREATE INDEX idx_multi_agent_records_tenant_created ON multi_agent_records(tenant_id, created_at DESC);
-- Run-scoped listing groups all collaboration records of one agent run.
CREATE INDEX idx_multi_agent_records_run ON multi_agent_records(run_id);
-- Kind-scoped listing filters by collaboration type within a tenant.
CREATE INDEX idx_multi_agent_records_tenant_kind ON multi_agent_records(tenant_id, kind);

-- Workflows: a saved, triggerable graph of "when X, run Y".
--
-- The canvas revamp needs somewhere to keep an authored graph. Everything a
-- workflow does already exists — cron scheduling (internal/cron), channel
-- events, agents, delegation, tool policy — but nothing lets a USER compose
-- those into a durable "every Monday, research competitors and put it in a deck".
-- That composition is what this table stores.
--
-- DESIGN: the graph is the source of TRUTH FOR AUTHORING ONLY. It is compiled
-- into the primitives that already run things (cron entries today; channel
-- subscriptions later), and those primitives remain authoritative at execution
-- time. Two reasons: a half-written graph must never be able to stop an existing
-- schedule from firing, and a workflow that is deleted must not leave orphaned
-- behaviour behind — the compile step owns that reconciliation explicitly rather
-- than the runtime learning to read graphs.
--
-- `graph` is JSONB rather than normalised node/edge tables on purpose. Nodes and
-- edges are edited as a WHOLE by the canvas (one save per drag), they are never
-- queried across workflows, and their shape will change every phase of this
-- revamp. Normalising now would buy joins nobody runs and force a migration for
-- every UI iteration. The trade-off is that the DB cannot enforce graph validity,
-- so the compile step validates and refuses to arm an invalid graph.

BEGIN;

CREATE TABLE IF NOT EXISTS workflows (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Tenant-scoped, NOT NULL: unlike cli_connections there is no such thing as a
    -- platform-provided workflow — a workflow names a tenant's own agents, so a
    -- global row could not resolve them.
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT,
    -- Authoring state. `enabled=false` means "saved but not armed": the canvas
    -- can be edited freely without anything firing, which is the normal state
    -- while someone is building. Arming is a separate, deliberate act.
    enabled     BOOLEAN NOT NULL DEFAULT FALSE,
    -- {nodes:[{id,type,position,data}], edges:[{id,source,target,...}]}
    graph       JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- What the last successful compile produced, so the reconciler can retract
    -- exactly what it created (e.g. the cron entry ids) instead of guessing from
    -- the current graph — which may already have been edited.
    compiled    JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- Why the last compile failed, surfaced in the UI. NULL = last compile was
    -- clean (or none has run).
    compile_error TEXT,
    created_by  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- One workflow name per tenant: the canvas addresses workflows by name in copy
-- ("Competitor digest is armed"), and two identically named rows make every such
-- message ambiguous.
CREATE UNIQUE INDEX IF NOT EXISTS workflows_tenant_name_idx
    ON workflows (tenant_id, lower(name));

-- The list query: a tenant's workflows, most recently touched first.
CREATE INDEX IF NOT EXISTS workflows_tenant_updated_idx
    ON workflows (tenant_id, updated_at DESC);

-- The reconciler's query on startup: every armed workflow, across tenants.
CREATE INDEX IF NOT EXISTS workflows_enabled_idx
    ON workflows (enabled) WHERE enabled;

COMMIT;

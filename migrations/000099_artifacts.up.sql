-- First-class persisted objects an agent produces (plan, patch, code, report,
-- review, research, architecture, ADR, test report, deployment plan). One row
-- per version; parent_id self-FK links a revision to its predecessor so the
-- version chain can be walked. Content is stored inline with a SHA-256
-- checksum for integrity verification (no external blob store).
CREATE TABLE artifacts (
    id           UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id    UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    run_id       TEXT,
    version      INT NOT NULL DEFAULT 1,
    author_agent TEXT,
    type         VARCHAR(40) NOT NULL,
    status       VARCHAR(40) NOT NULL DEFAULT 'draft',
    checksum     TEXT,
    parent_id    UUID REFERENCES artifacts(id) ON DELETE SET NULL,
    title        TEXT,
    content      TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Tenant-scoped listing is the primary read path.
CREATE INDEX idx_artifacts_tenant_created ON artifacts(tenant_id, created_at DESC);
-- Run-scoped listing groups all artifacts produced during one agent run.
CREATE INDEX idx_artifacts_run ON artifacts(run_id);
-- Version-chain walk: children of a parent, or roots when parent_id IS NULL.
CREATE INDEX idx_artifacts_tenant_parent ON artifacts(tenant_id, parent_id);

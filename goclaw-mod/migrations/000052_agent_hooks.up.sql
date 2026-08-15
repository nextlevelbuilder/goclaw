-- Migration 000052: Agent hooks system
-- Creates agent_hooks, hook_executions, and tenant_hook_budget tables.
-- scope uses CHECK-based enum (no separate ENUM type) for portability.
-- Global-scope hooks use the MasterTenantID (0193a5b0-7000-7000-8000-000000000001)
-- as tenant_id. This aligns with store.MasterTenantID / store.IsMasterScope
-- conventions and satisfies any future FK to tenants(id).

-- ============================================================
-- Table: agent_hooks
-- ============================================================

CREATE TABLE IF NOT EXISTS agent_hooks (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID NOT NULL DEFAULT '0193a5b0-7000-7000-8000-000000000001',
    agent_id     UUID REFERENCES agents(id) ON DELETE CASCADE,
    scope        VARCHAR(8) NOT NULL CHECK (scope IN ('global', 'tenant', 'agent')),
    event        VARCHAR(32) NOT NULL,
    handler_type VARCHAR(16) NOT NULL CHECK (handler_type IN ('command', 'http', 'prompt')),
    -- config holds handler-specific options (e.g. command path, http url, prompt template).
    config       JSONB NOT NULL DEFAULT '{}',
    -- matcher is an optional regex applied to tool_name before the hook fires.
    matcher      VARCHAR(256),
    -- if_expr is an optional CEL expression evaluated against tool_input.
    if_expr      TEXT,
    timeout_ms   INT NOT NULL DEFAULT 5000,
    on_timeout   VARCHAR(8) NOT NULL DEFAULT 'block' CHECK (on_timeout IN ('block', 'allow')),
    priority     INT NOT NULL DEFAULT 0,
    enabled      BOOL NOT NULL DEFAULT TRUE,
    version      INT NOT NULL DEFAULT 1,
    -- source: 'ui' (dashboard), 'api', 'seed' (bootstrap).
    source       VARCHAR(8) NOT NULL DEFAULT 'ui' CHECK (source IN ('ui', 'api', 'seed')),
    -- metadata stores UI-only fields (tags, notes, lastTestedAt, createdByUsername)
    -- and is extensible without future migrations. Project convention: always present.
    metadata     JSONB NOT NULL DEFAULT '{}',
    created_by   UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Partial unique indexes per scope prevent duplicate hooks within the same scope
-- while allowing the same (event, handler_type) across different scopes (C3 fix).
CREATE UNIQUE INDEX IF NOT EXISTS uq_hooks_global
    ON agent_hooks (event, handler_type)
    WHERE scope = 'global';

CREATE UNIQUE INDEX IF NOT EXISTS uq_hooks_tenant
    ON agent_hooks (tenant_id, event, handler_type)
    WHERE scope = 'tenant';

CREATE UNIQUE INDEX IF NOT EXISTS uq_hooks_agent
    ON agent_hooks (tenant_id, agent_id, event, handler_type)
    WHERE scope = 'agent';

-- idx_hooks_lookup: hot-path index for ResolveForEvent queries.
-- Partial WHERE enabled reduces index size (most production rows will be enabled).
CREATE INDEX IF NOT EXISTS idx_hooks_lookup
    ON agent_hooks (tenant_id, agent_id, event)
    WHERE enabled = TRUE;

-- ============================================================
-- Table: hook_executions (append-only audit log)
-- ============================================================

CREATE TABLE IF NOT EXISTS hook_executions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- hook_id is SET NULL when the parent hook is deleted (preserves audit trail).
    hook_id     UUID REFERENCES agent_hooks(id) ON DELETE SET NULL,
    session_id  VARCHAR(500),
    event       VARCHAR(32) NOT NULL,
    -- input_hash is canonical-JSON sha256 (64 hex chars) of (tool_name + sorted args).
    input_hash  CHAR(64),
    decision    VARCHAR(16) NOT NULL CHECK (decision IN ('allow', 'block', 'error', 'timeout')),
    duration_ms INT NOT NULL DEFAULT 0,
    retry       INT NOT NULL DEFAULT 0,
    -- dedup_key prevents duplicate execution rows for the same (hook_id, event_id).
    dedup_key   VARCHAR(128),
    -- error truncated to 256 chars at write time (M2 mitigation).
    error       VARCHAR(256),
    -- error_detail stores the full error AES-256-GCM encrypted; GDPR-purgeable.
    error_detail BYTEA,
    -- metadata stores extensible exec context: matcher_matched, cel_eval_result,
    -- stdout_len, http_status, prompt_model, prompt_tokens, trace_id. Avoids
    -- future migration for new observability fields.
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_hook_executions_session
    ON hook_executions (session_id, created_at);

CREATE UNIQUE INDEX IF NOT EXISTS uq_hook_executions_dedup
    ON hook_executions (dedup_key)
    WHERE dedup_key IS NOT NULL;

-- ============================================================
-- Table: tenant_hook_budget
-- Consolidated here (Phase 3 prompt handler atomic deduct).
-- One row per tenant tracks monthly spend against a cap.
-- ============================================================

CREATE TABLE IF NOT EXISTS tenant_hook_budget (
    tenant_id    UUID PRIMARY KEY,
    month_start  DATE NOT NULL,
    budget_total BIGINT NOT NULL DEFAULT 0,
    remaining    BIGINT NOT NULL DEFAULT 0,
    last_warned_at TIMESTAMPTZ,
    -- metadata stores alert thresholds, override flags, notes.
    metadata     JSONB NOT NULL DEFAULT '{}',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

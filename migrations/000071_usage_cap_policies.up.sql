CREATE TABLE IF NOT EXISTS usage_cap_policies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    agent_id UUID REFERENCES agents(id) ON DELETE CASCADE,
    provider_id UUID REFERENCES llm_providers(id) ON DELETE CASCADE,
    provider_type TEXT,
    model_id TEXT,
    window_key TEXT NOT NULL CHECK (window_key IN ('hour', 'day', 'week', 'month')),
    max_tokens BIGINT CHECK (max_tokens IS NULL OR max_tokens >= 0),
    max_cost_micros BIGINT CHECK (max_cost_micros IS NULL OR max_cost_micros >= 0),
    enabled BOOLEAN NOT NULL DEFAULT true,
    priority INTEGER NOT NULL DEFAULT 100,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (max_tokens IS NOT NULL OR max_cost_micros IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS idx_usage_cap_policies_scope
    ON usage_cap_policies (tenant_id, enabled, agent_id, provider_id, provider_type, model_id);

CREATE TABLE IF NOT EXISTS usage_cap_counters (
    policy_id UUID NOT NULL REFERENCES usage_cap_policies(id) ON DELETE CASCADE,
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    used_tokens BIGINT NOT NULL DEFAULT 0,
    reserved_tokens BIGINT NOT NULL DEFAULT 0,
    used_cost_micros BIGINT NOT NULL DEFAULT 0,
    reserved_cost_micros BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (policy_id, window_start)
);

CREATE TABLE IF NOT EXISTS usage_cap_reservations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reservation_key TEXT NOT NULL,
    policy_id UUID NOT NULL REFERENCES usage_cap_policies(id) ON DELETE CASCADE,
    window_start TIMESTAMPTZ NOT NULL,
    reserved_tokens BIGINT NOT NULL DEFAULT 0,
    reserved_cost_micros BIGINT NOT NULL DEFAULT 0,
    actual_tokens BIGINT NOT NULL DEFAULT 0,
    actual_cost_micros BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'reserved',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (reservation_key, policy_id)
);

CREATE INDEX IF NOT EXISTS idx_usage_cap_reservations_key
    ON usage_cap_reservations (reservation_key);

CREATE TABLE IF NOT EXISTS usage_cap_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    policy_id UUID REFERENCES usage_cap_policies(id) ON DELETE SET NULL,
    reservation_key TEXT,
    decision TEXT NOT NULL,
    reason TEXT,
    estimated_tokens BIGINT NOT NULL DEFAULT 0,
    estimated_cost_micros BIGINT NOT NULL DEFAULT 0,
    actual_tokens BIGINT NOT NULL DEFAULT 0,
    actual_cost_micros BIGINT NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_usage_cap_events_tenant_created
    ON usage_cap_events (tenant_id, created_at DESC);

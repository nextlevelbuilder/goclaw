CREATE TABLE IF NOT EXISTS usage_pricing_catalog (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model_id TEXT NOT NULL UNIQUE,
    canonical_model_id TEXT,
    raw_pricing JSONB NOT NULL DEFAULT '{}'::jsonb,
    raw_model JSONB NOT NULL DEFAULT '{}'::jsonb,
    input_price NUMERIC(30, 18) CHECK (input_price IS NULL OR input_price >= 0),
    output_price NUMERIC(30, 18) CHECK (output_price IS NULL OR output_price >= 0),
    cache_read_price NUMERIC(30, 18) CHECK (cache_read_price IS NULL OR cache_read_price >= 0),
    cache_write_price NUMERIC(30, 18) CHECK (cache_write_price IS NULL OR cache_write_price >= 0),
    reasoning_price NUMERIC(30, 18) CHECK (reasoning_price IS NULL OR reasoning_price >= 0),
    request_price NUMERIC(30, 18) CHECK (request_price IS NULL OR request_price >= 0),
    image_price NUMERIC(30, 18) CHECK (image_price IS NULL OR image_price >= 0),
    web_search_price NUMERIC(30, 18) CHECK (web_search_price IS NULL OR web_search_price >= 0),
    synced_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_usage_pricing_catalog_synced_at
    ON usage_pricing_catalog (synced_at DESC);

CREATE TABLE IF NOT EXISTS usage_pricing_overrides (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    provider_id UUID NOT NULL REFERENCES llm_providers(id) ON DELETE CASCADE,
    provider_type TEXT NOT NULL,
    model_id TEXT NOT NULL,
    input_price NUMERIC(30, 18) CHECK (input_price IS NULL OR input_price >= 0),
    output_price NUMERIC(30, 18) CHECK (output_price IS NULL OR output_price >= 0),
    cache_read_price NUMERIC(30, 18) CHECK (cache_read_price IS NULL OR cache_read_price >= 0),
    cache_write_price NUMERIC(30, 18) CHECK (cache_write_price IS NULL OR cache_write_price >= 0),
    reasoning_price NUMERIC(30, 18) CHECK (reasoning_price IS NULL OR reasoning_price >= 0),
    request_price NUMERIC(30, 18) CHECK (request_price IS NULL OR request_price >= 0),
    image_price NUMERIC(30, 18) CHECK (image_price IS NULL OR image_price >= 0),
    web_search_price NUMERIC(30, 18) CHECK (web_search_price IS NULL OR web_search_price >= 0),
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, provider_id, model_id)
);

CREATE INDEX IF NOT EXISTS idx_usage_pricing_overrides_tenant_provider
    ON usage_pricing_overrides (tenant_id, provider_id, model_id)
    WHERE enabled;

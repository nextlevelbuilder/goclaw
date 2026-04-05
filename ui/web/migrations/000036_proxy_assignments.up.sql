-- Add enabled toggle to proxies
ALTER TABLE browser_proxies ADD COLUMN is_enabled BOOLEAN NOT NULL DEFAULT true;

-- Sticky proxy-profile assignments
CREATE TABLE proxy_profile_assignments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL,
    proxy_id UUID NOT NULL REFERENCES browser_proxies(id) ON DELETE CASCADE,
    profile_dir TEXT NOT NULL,
    assigned_at TIMESTAMPTZ DEFAULT now(),
    last_used_at TIMESTAMPTZ DEFAULT now(),
    UNIQUE(tenant_id, profile_dir)
);
CREATE INDEX idx_proxy_profile_tenant ON proxy_profile_assignments(tenant_id);
CREATE INDEX idx_proxy_profile_proxy ON proxy_profile_assignments(proxy_id);

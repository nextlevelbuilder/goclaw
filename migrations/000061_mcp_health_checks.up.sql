-- MCP health check history: records every health ping result for each server.
CREATE TABLE mcp_health_checks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id UUID REFERENCES mcp_servers(id) ON DELETE CASCADE,
    server_name VARCHAR(255) NOT NULL,
    tenant_id UUID NOT NULL,
    status VARCHAR(20) NOT NULL,      -- "healthy", "unhealthy", "reconnecting"
    latency_ms INTEGER,               -- ping round-trip (null for unhealthy)
    error TEXT,                        -- error message (null for healthy)
    tool_count INTEGER DEFAULT 0,
    health_failures INTEGER DEFAULT 0,
    checked_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_mcp_health_server_time ON mcp_health_checks (server_id, checked_at DESC);
CREATE INDEX idx_mcp_health_tenant_time ON mcp_health_checks (tenant_id, checked_at DESC);

CREATE TABLE mcp_context_grants (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_instance_id UUID NOT NULL REFERENCES channel_instances(id) ON DELETE CASCADE,
    scope_type VARCHAR(32) NOT NULL,
    scope_key VARCHAR(255) NOT NULL DEFAULT '',
    server_id UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT true,
    tool_allow JSONB,
    tool_deny JSONB,
    config_overrides JSONB,
    granted_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, channel_instance_id, scope_type, scope_key, server_id)
);

CREATE INDEX idx_mcp_context_grants_scope ON mcp_context_grants(tenant_id, channel_instance_id, scope_type, scope_key);
CREATE INDEX idx_mcp_context_grants_server ON mcp_context_grants(server_id);

CREATE TABLE mcp_context_credentials (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_instance_id UUID NOT NULL REFERENCES channel_instances(id) ON DELETE CASCADE,
    scope_type VARCHAR(32) NOT NULL,
    scope_key VARCHAR(255) NOT NULL DEFAULT '',
    server_id UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    api_key TEXT,
    headers BYTEA,
    env BYTEA,
    created_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, channel_instance_id, scope_type, scope_key, server_id)
);

CREATE INDEX idx_mcp_context_credentials_scope ON mcp_context_credentials(tenant_id, channel_instance_id, scope_type, scope_key);
CREATE INDEX idx_mcp_context_credentials_server ON mcp_context_credentials(server_id);

CREATE TABLE secure_cli_context_grants (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_instance_id UUID NOT NULL REFERENCES channel_instances(id) ON DELETE CASCADE,
    scope_type VARCHAR(32) NOT NULL,
    scope_key VARCHAR(255) NOT NULL DEFAULT '',
    binary_id UUID NOT NULL REFERENCES secure_cli_binaries(id) ON DELETE CASCADE,
    deny_args JSONB,
    deny_verbose JSONB,
    timeout_seconds INTEGER,
    tips TEXT,
    encrypted_env BYTEA,
    enabled BOOLEAN NOT NULL DEFAULT true,
    granted_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, channel_instance_id, scope_type, scope_key, binary_id)
);

CREATE INDEX idx_secure_cli_context_grants_scope ON secure_cli_context_grants(tenant_id, channel_instance_id, scope_type, scope_key);
CREATE INDEX idx_secure_cli_context_grants_binary ON secure_cli_context_grants(binary_id);

CREATE TABLE secure_cli_context_credentials (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    channel_instance_id UUID NOT NULL REFERENCES channel_instances(id) ON DELETE CASCADE,
    scope_type VARCHAR(32) NOT NULL,
    scope_key VARCHAR(255) NOT NULL DEFAULT '',
    binary_id UUID NOT NULL REFERENCES secure_cli_binaries(id) ON DELETE CASCADE,
    encrypted_env BYTEA NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    credential_type TEXT,
    host_scope TEXT,
    created_by VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(tenant_id, channel_instance_id, scope_type, scope_key, binary_id)
);

CREATE INDEX idx_secure_cli_context_credentials_scope ON secure_cli_context_credentials(tenant_id, channel_instance_id, scope_type, scope_key);
CREATE INDEX idx_secure_cli_context_credentials_binary ON secure_cli_context_credentials(binary_id);

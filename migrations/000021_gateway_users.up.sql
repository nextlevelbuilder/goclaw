-- Gateway users: multi-tenant user management with role-based access.
-- Root user is auto-seeded from GOCLAW_GATEWAY_TOKEN env var on first boot.
CREATE TABLE gateway_users (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    user_id         VARCHAR(255) NOT NULL UNIQUE,            -- unique login identifier
    gateway_token   VARCHAR(255) NOT NULL UNIQUE,            -- authentication token
    role            VARCHAR(50)  NOT NULL DEFAULT 'admin',   -- 'root' or 'admin'
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Fast lookup by gateway token (authentication)
CREATE INDEX idx_gateway_users_token ON gateway_users (gateway_token);

-- Fast lookup by role
CREATE INDEX idx_gateway_users_role ON gateway_users (role);

-- Users table: tracks authenticated users and their last login time.
-- The id is the Keycloak subject (sub claim) or any external identity.

CREATE TABLE IF NOT EXISTS users (
    id            TEXT        PRIMARY KEY,
    last_login_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS role         TEXT    NOT NULL DEFAULT 'operator',
    ADD COLUMN IF NOT EXISTS display_name TEXT;

-- Add constraint only if not exists (idempotent)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'users_role_check'
    ) THEN
        ALTER TABLE users ADD CONSTRAINT users_role_check
            CHECK (role IN ('admin', 'operator', 'viewer'));
    END IF;
END
$$;

CREATE INDEX IF NOT EXISTS idx_users_role ON users(role);
CREATE INDEX IF NOT EXISTS idx_users_last_login ON users(last_login_at DESC);

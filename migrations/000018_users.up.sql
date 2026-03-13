-- Users table: tracks authenticated users and their last login time.
-- The id is the Keycloak subject (sub claim) or any external identity.

CREATE TABLE IF NOT EXISTS users (
    id            TEXT        PRIMARY KEY,
    last_login_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

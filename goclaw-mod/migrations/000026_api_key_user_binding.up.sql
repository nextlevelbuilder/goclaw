-- API key owner binding for identity enforcement
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS owner_id VARCHAR(255);
COMMENT ON COLUMN api_keys.owner_id IS 'User who owns this key. When set, auth via this key forces user_id = owner_id.';
CREATE INDEX IF NOT EXISTS idx_api_keys_owner_id ON api_keys(owner_id) WHERE owner_id IS NOT NULL;

-- Team user grants for access control
CREATE TABLE IF NOT EXISTS team_user_grants (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    team_id    UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    user_id    VARCHAR(255) NOT NULL,
    role       VARCHAR(50) NOT NULL DEFAULT 'viewer',
    granted_by VARCHAR(255),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(team_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_team_user_grants_user ON team_user_grants(user_id);
CREATE INDEX IF NOT EXISTS idx_team_user_grants_team ON team_user_grants(team_id);

-- Drop unused legacy tables
DROP TABLE IF EXISTS handoff_routes;
DROP TABLE IF EXISTS delegation_history;

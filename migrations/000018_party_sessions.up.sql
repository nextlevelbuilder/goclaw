-- 000014_party_sessions.up.sql
CREATE TABLE IF NOT EXISTS party_sessions (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    topic       TEXT NOT NULL,
    team_preset VARCHAR(50),
    status      VARCHAR(20) NOT NULL DEFAULT 'assembling',
    mode        VARCHAR(10) NOT NULL DEFAULT 'standard',
    round       INT NOT NULL DEFAULT 0,
    max_rounds  INT NOT NULL DEFAULT 10,
    user_id     VARCHAR(200) NOT NULL,
    channel     VARCHAR(255),
    chat_id     VARCHAR(200),
    personas    JSONB NOT NULL DEFAULT '[]',
    context     JSONB NOT NULL DEFAULT '{}',
    history     JSONB NOT NULL DEFAULT '[]',
    summary     JSONB,
    artifacts   JSONB NOT NULL DEFAULT '[]',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_party_sessions_user ON party_sessions(user_id, status);
CREATE INDEX IF NOT EXISTS idx_party_sessions_channel ON party_sessions(channel, chat_id, status);

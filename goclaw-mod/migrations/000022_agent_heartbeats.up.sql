-- Agent heartbeat configuration (per-agent, not per-user).
CREATE TABLE agent_heartbeats (
    id                 UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id           UUID NOT NULL UNIQUE REFERENCES agents(id) ON DELETE CASCADE,
    enabled            BOOLEAN NOT NULL DEFAULT false,
    interval_sec       INT NOT NULL DEFAULT 1800,
    prompt             TEXT,
    provider_id        UUID REFERENCES llm_providers(id),
    model              VARCHAR(200),
    isolated_session   BOOLEAN NOT NULL DEFAULT true,
    light_context      BOOLEAN NOT NULL DEFAULT false,
    ack_max_chars      INT NOT NULL DEFAULT 300,
    max_retries        INT NOT NULL DEFAULT 2,
    active_hours_start VARCHAR(5),
    active_hours_end   VARCHAR(5),
    timezone           TEXT,
    channel            VARCHAR(50),
    chat_id            TEXT,
    next_run_at        TIMESTAMPTZ,
    last_run_at        TIMESTAMPTZ,
    last_status        VARCHAR(20),
    last_error         TEXT,
    run_count          INT NOT NULL DEFAULT 0,
    suppress_count     INT NOT NULL DEFAULT 0,
    metadata           JSONB DEFAULT '{}',
    created_at         TIMESTAMPTZ DEFAULT NOW(),
    updated_at         TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_heartbeats_due ON agent_heartbeats (next_run_at)
    WHERE enabled = true AND next_run_at IS NOT NULL;

-- Heartbeat execution logs.
CREATE TABLE heartbeat_run_logs (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    heartbeat_id   UUID NOT NULL REFERENCES agent_heartbeats(id) ON DELETE CASCADE,
    agent_id       UUID NOT NULL REFERENCES agents(id),
    status         VARCHAR(20) NOT NULL,
    summary        TEXT,
    error          TEXT,
    duration_ms    INT,
    input_tokens   INT DEFAULT 0,
    output_tokens  INT DEFAULT 0,
    skip_reason    VARCHAR(50),
    metadata       JSONB DEFAULT '{}',
    ran_at         TIMESTAMPTZ DEFAULT NOW(),
    created_at     TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_hb_logs_heartbeat ON heartbeat_run_logs (heartbeat_id, ran_at DESC);
CREATE INDEX idx_hb_logs_agent ON heartbeat_run_logs (agent_id, ran_at DESC);

-- Generic agent config permissions (heartbeat, cron, context_files, etc.)
CREATE TABLE agent_config_permissions (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    scope       VARCHAR(100) NOT NULL,
    config_type VARCHAR(50) NOT NULL,
    user_id     VARCHAR(255) NOT NULL,
    permission  VARCHAR(10) NOT NULL,
    granted_by  VARCHAR(255),
    metadata    JSONB DEFAULT '{}',
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    updated_at  TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(agent_id, scope, config_type, user_id)
);

CREATE INDEX idx_acp_lookup ON agent_config_permissions (agent_id, scope, config_type);

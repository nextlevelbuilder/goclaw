-- ============================================================
-- Agent Teams (collaborative multi-agent coordination)
-- ============================================================

CREATE TABLE agent_teams (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    name          VARCHAR(255) NOT NULL,
    lead_agent_id UUID NOT NULL REFERENCES agents(id),
    description   TEXT,
    status        VARCHAR(20) NOT NULL DEFAULT 'active',
    settings      JSONB NOT NULL DEFAULT '{}',
    created_by    VARCHAR(255) NOT NULL,
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    updated_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE agent_team_members (
    team_id   UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    agent_id  UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    role      VARCHAR(20) NOT NULL DEFAULT 'member',
    joined_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (team_id, agent_id)
);

-- ============================================================
-- Team Tasks (shared task list with self-claim + dependencies)
-- ============================================================

CREATE TABLE team_tasks (
    id             UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    team_id        UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    subject        VARCHAR(500) NOT NULL,
    description    TEXT,
    status         VARCHAR(20) NOT NULL DEFAULT 'pending',
    owner_agent_id UUID REFERENCES agents(id),
    blocked_by     UUID[],
    priority       INT NOT NULL DEFAULT 0,
    result         TEXT,
    created_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at     TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_team_tasks_team ON team_tasks(team_id);
CREATE INDEX idx_team_tasks_status ON team_tasks(team_id, status);

-- ============================================================
-- Team Messages (peer-to-peer mailbox)
-- ============================================================

CREATE TABLE team_messages (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    team_id       UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    from_agent_id UUID NOT NULL REFERENCES agents(id),
    to_agent_id   UUID REFERENCES agents(id),
    content       TEXT NOT NULL,
    message_type  VARCHAR(30) NOT NULL DEFAULT 'chat',
    read          BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_team_messages_to ON team_messages(team_id, to_agent_id, read);

-- ============================================================
-- Link agent_links to teams: team-created links have team_id set.
-- ON DELETE SET NULL → when team is deleted, links become manual.
-- ============================================================

ALTER TABLE agent_links ADD COLUMN IF NOT EXISTS team_id UUID REFERENCES agent_teams(id) ON DELETE SET NULL;

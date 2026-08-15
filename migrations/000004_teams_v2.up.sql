-- 000004_teams_v2: FTS for team_tasks + delegation_history table

-- 1. Full-text search on team_tasks (subject + description)
ALTER TABLE team_tasks ADD COLUMN IF NOT EXISTS tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', subject || ' ' || COALESCE(description, ''))) STORED;
CREATE INDEX IF NOT EXISTS idx_team_tasks_tsv ON team_tasks USING GIN(tsv);

-- 2. Delegation history (persisted record of every delegation)
CREATE TABLE IF NOT EXISTS delegation_history (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    source_agent_id UUID NOT NULL REFERENCES agents(id),
    target_agent_id UUID NOT NULL REFERENCES agents(id),
    team_id         UUID REFERENCES agent_teams(id) ON DELETE SET NULL,
    team_task_id    UUID REFERENCES team_tasks(id) ON DELETE SET NULL,
    user_id         VARCHAR(255),
    task            TEXT NOT NULL,
    mode            VARCHAR(10) NOT NULL DEFAULT 'sync',
    status          VARCHAR(20) NOT NULL DEFAULT 'completed',
    result          TEXT,
    error           TEXT,
    iterations      INT DEFAULT 0,
    trace_id        UUID,
    duration_ms     INT,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    completed_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_delegation_history_source ON delegation_history(source_agent_id);
CREATE INDEX IF NOT EXISTS idx_delegation_history_team ON delegation_history(team_id);
CREATE INDEX IF NOT EXISTS idx_delegation_history_created ON delegation_history(created_at DESC);

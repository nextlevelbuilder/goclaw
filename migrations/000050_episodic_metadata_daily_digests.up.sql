-- Phase 2: Add metadata to episodic_summaries for structured extraction
-- Phase 3: Add daily_digests table for aggregated daily reports

-- Add metadata JSONB column for decisions, actions, entities extracted from summaries
ALTER TABLE episodic_summaries ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}';

-- Create daily_digests table for end-of-day aggregation
-- Replaces manual "daily log" cron jobs with structured, queryable data
CREATE TABLE IF NOT EXISTS daily_digests (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id UUID NOT NULL REFERENCES tenants(id),
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id VARCHAR(255) NOT NULL DEFAULT '',

    -- Scope: which channel/group this digest covers (empty = agent-wide)
    channel_scope VARCHAR(100) DEFAULT '',
    session_key_prefix TEXT DEFAULT '',  -- e.g., "agent:X:channel:group:-123"

    -- Date this digest covers
    digest_date DATE NOT NULL,

    -- Structured content extracted from episodic summaries
    decisions JSONB DEFAULT '[]',      -- [{id, content, status, source_session}]
    action_items JSONB DEFAULT '[]',   -- [{id, content, assignee, due_date, status}]
    key_topics TEXT[] DEFAULT '{}',
    summary TEXT DEFAULT '',           -- LLM-generated daily summary

    -- Stats
    session_count INTEGER DEFAULT 0,
    message_count INTEGER DEFAULT 0,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- One digest per agent+user+date+scope
    UNIQUE(tenant_id, agent_id, user_id, digest_date, channel_scope)
);

-- Indexes for daily_digests
CREATE INDEX IF NOT EXISTS idx_daily_digests_agent_date ON daily_digests(agent_id, digest_date DESC);
CREATE INDEX IF NOT EXISTS idx_daily_digests_tenant ON daily_digests(tenant_id);
CREATE INDEX IF NOT EXISTS idx_daily_digests_lookup ON daily_digests(agent_id, user_id, digest_date DESC);

COMMENT ON TABLE daily_digests IS 'Aggregated daily reports from episodic summaries. Replaces manual daily log cron jobs.';
COMMENT ON COLUMN episodic_summaries.metadata IS 'Structured extraction: decisions, actions, entities from session summary.';

-- Add promoted_at column to episodic_summaries for dreaming pipeline.
-- NULL = not yet promoted to long-term memory; NOT NULL = already processed.
ALTER TABLE episodic_summaries ADD COLUMN IF NOT EXISTS promoted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_episodic_unpromoted
    ON episodic_summaries(agent_id, user_id, created_at)
    WHERE promoted_at IS NULL;

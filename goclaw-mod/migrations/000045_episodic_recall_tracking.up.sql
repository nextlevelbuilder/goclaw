-- Phase 10: dreaming weighted scoring. Track per-episode recall signals
-- that feed ComputeRecallScore() in internal/consolidation/scoring.go.
-- recall_score stores the running-average of search hit scores so the
-- dreaming worker can sort unpromoted entries by perceived usefulness
-- instead of strictly oldest-first.
ALTER TABLE episodic_summaries ADD COLUMN IF NOT EXISTS recall_count INT DEFAULT 0 NOT NULL;
ALTER TABLE episodic_summaries ADD COLUMN IF NOT EXISTS recall_score DOUBLE PRECISION DEFAULT 0 NOT NULL;
ALTER TABLE episodic_summaries ADD COLUMN IF NOT EXISTS last_recalled_at TIMESTAMPTZ;

-- Partial index for DreamingWorker.ListUnpromotedScored — only touches
-- unpromoted rows and orders by recall_score DESC to match the primary
-- query shape: "top-N unpromoted with highest recall signal".
CREATE INDEX IF NOT EXISTS idx_episodic_recall_unpromoted
    ON episodic_summaries(agent_id, user_id, recall_score DESC)
    WHERE promoted_at IS NULL;

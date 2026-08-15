DROP INDEX IF EXISTS idx_episodic_recall_unpromoted;
ALTER TABLE episodic_summaries DROP COLUMN IF EXISTS last_recalled_at;
ALTER TABLE episodic_summaries DROP COLUMN IF EXISTS recall_score;
ALTER TABLE episodic_summaries DROP COLUMN IF EXISTS recall_count;

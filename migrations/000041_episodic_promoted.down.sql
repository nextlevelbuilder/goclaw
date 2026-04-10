DROP INDEX IF EXISTS idx_episodic_unpromoted;
ALTER TABLE episodic_summaries DROP COLUMN IF EXISTS promoted_at;

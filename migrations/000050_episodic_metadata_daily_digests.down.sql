-- Rollback Phase 2 & 3

DROP TABLE IF EXISTS daily_digests;
ALTER TABLE episodic_summaries DROP COLUMN IF EXISTS metadata;

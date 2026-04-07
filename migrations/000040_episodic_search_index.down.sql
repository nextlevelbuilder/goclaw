-- Migration 000040 rollback: remove episodic search index additions.

DROP INDEX IF EXISTS idx_episodic_embedding_hnsw;
DROP INDEX IF EXISTS idx_episodic_search_vector;
ALTER TABLE episodic_summaries DROP COLUMN IF EXISTS search_vector;

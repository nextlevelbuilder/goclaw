DROP TABLE IF EXISTS kg_dedup_candidates;
DROP INDEX IF EXISTS idx_kg_entities_tsv;
ALTER TABLE kg_entities DROP COLUMN IF EXISTS tsv;

-- Revert memory_chunks FTS config back to 'simple'.
DROP INDEX IF EXISTS idx_mem_tsv;
ALTER TABLE memory_chunks DROP COLUMN IF EXISTS tsv;
ALTER TABLE memory_chunks ADD COLUMN tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', text)) STORED;
CREATE INDEX idx_mem_tsv ON memory_chunks USING GIN(tsv);

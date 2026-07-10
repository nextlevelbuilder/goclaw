-- memory_chunks FTS: switch tsvector config 'simple' -> 'russian' so recall
-- survives Russian morphology (grammatical cases). Under 'simple' there is no
-- stemming, so a genitive query token ("Дианы") never matches the stored
-- nominative token ("Диана"). The Snowball 'russian' stemmer reduces both to
-- the same lexeme ("диан"), fixing case-insensitive fact recall.
--
-- A GENERATED column's expression cannot be ALTERed in place, so the column is
-- dropped and re-added; Postgres recomputes tsv for all existing rows on ADD
-- (automatic backfill). The GIN index is dropped/recreated with it.
-- memory_search.go queries switch to plainto_tsquery('russian', ...) to match.
DROP INDEX IF EXISTS idx_mem_tsv;
ALTER TABLE memory_chunks DROP COLUMN IF EXISTS tsv;
ALTER TABLE memory_chunks ADD COLUMN tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('russian', text)) STORED;
CREATE INDEX idx_mem_tsv ON memory_chunks USING GIN(tsv);

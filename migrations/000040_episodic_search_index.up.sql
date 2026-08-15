-- Migration 000040: Episodic search index
-- Adds stored tsvector column for full-text search and an optimized HNSW vector index.
-- Note: promoted_at column is added in migration 000041.

-- Immutable wrapper: array_to_string is STABLE in PG, but the expression is
-- effectively immutable for our use (no locale-dependent behavior on text[]).
-- Generated columns require IMMUTABLE expressions.
CREATE OR REPLACE FUNCTION immutable_array_to_string(arr text[], sep text)
RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE AS
$$SELECT array_to_string(arr, sep)$$;

ALTER TABLE episodic_summaries ADD COLUMN IF NOT EXISTS search_vector tsvector
  GENERATED ALWAYS AS (to_tsvector('english'::regconfig, coalesce(summary, '') || ' ' || coalesce(immutable_array_to_string(key_topics, ' '), ''))) STORED;

CREATE INDEX IF NOT EXISTS idx_episodic_search_vector ON episodic_summaries USING GIN (search_vector);
CREATE INDEX IF NOT EXISTS idx_episodic_embedding_hnsw ON episodic_summaries USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64) WHERE embedding IS NOT NULL;

-- Obsidian frontmatter metadata for memory documents.
-- Parsed by internal/memory/frontmatter.go and persisted as JSONB so
-- consumers can query by tags / aliases / type / sources without an
-- additional table. Empty object when the doc had no frontmatter.
ALTER TABLE memory_documents
  ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

-- GIN index for tag / type containment queries (e.g. "all docs tagged
-- voice"). Skips empty-metadata rows to keep the index lean.
CREATE INDEX IF NOT EXISTS idx_memory_documents_metadata
  ON memory_documents USING GIN (metadata)
  WHERE metadata <> '{}'::jsonb;

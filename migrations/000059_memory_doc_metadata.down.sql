DROP INDEX IF EXISTS idx_memory_documents_metadata;
ALTER TABLE memory_documents DROP COLUMN IF EXISTS metadata;

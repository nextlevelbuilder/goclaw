DROP INDEX IF EXISTS idx_memdoc_session;
ALTER TABLE memory_documents DROP COLUMN IF EXISTS compact_number;
ALTER TABLE memory_documents DROP COLUMN IF EXISTS session_key;

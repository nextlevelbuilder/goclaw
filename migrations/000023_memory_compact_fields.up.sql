ALTER TABLE memory_documents ADD COLUMN IF NOT EXISTS session_key TEXT;
ALTER TABLE memory_documents ADD COLUMN IF NOT EXISTS compact_number INT;

CREATE INDEX IF NOT EXISTS idx_memdoc_session ON memory_documents(agent_id, session_key) WHERE session_key IS NOT NULL;

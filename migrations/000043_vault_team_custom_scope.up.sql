-- Add team_id to vault_documents (NULL = personal scope).
ALTER TABLE vault_documents ADD COLUMN IF NOT EXISTS team_id UUID
    REFERENCES agent_teams(id) ON DELETE SET NULL;

-- Add custom_scope for future flexibility.
ALTER TABLE vault_documents ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);

-- Drop old broken UNIQUE constraint that causes cross-team data corruption.
ALTER TABLE vault_documents DROP CONSTRAINT IF EXISTS vault_documents_agent_id_scope_path_key;

-- New UNIQUE with COALESCE: NULL team_id maps to nil-UUID so NULLs collapse correctly.
CREATE UNIQUE INDEX IF NOT EXISTS uq_vault_docs_agent_team_scope_path
    ON vault_documents (agent_id, COALESCE(team_id, '00000000-0000-0000-0000-000000000000'), scope, path);

-- Index for team_id filtering.
CREATE INDEX IF NOT EXISTS idx_vault_docs_team ON vault_documents(team_id) WHERE team_id IS NOT NULL;

-- Trigger: when team deleted (ON DELETE SET NULL), auto-correct scope to 'personal'.
-- Prevents orphaned scope='team' docs.
CREATE OR REPLACE FUNCTION vault_docs_team_null_scope_fix()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.team_id IS NULL AND OLD.team_id IS NOT NULL THEN
        NEW.scope := 'personal';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_vault_docs_team_null_scope
    BEFORE UPDATE OF team_id ON vault_documents
    FOR EACH ROW
    EXECUTE FUNCTION vault_docs_team_null_scope_fix();

-- Add custom_scope to 9 other tables.
ALTER TABLE vault_links ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE vault_versions ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE memory_documents ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE memory_chunks ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE team_tasks ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE team_task_attachments ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE team_task_comments ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE team_task_events ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);
ALTER TABLE subagent_tasks ADD COLUMN IF NOT EXISTS custom_scope VARCHAR(255);

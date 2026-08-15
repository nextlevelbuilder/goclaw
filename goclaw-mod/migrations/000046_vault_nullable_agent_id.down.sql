-- Revert: delete orphaned docs, restore NOT NULL + CASCADE.
DELETE FROM vault_documents WHERE agent_id IS NULL;

ALTER TABLE vault_documents ALTER COLUMN agent_id SET NOT NULL;

ALTER TABLE vault_documents DROP CONSTRAINT vault_documents_agent_id_fkey;
ALTER TABLE vault_documents ADD CONSTRAINT vault_documents_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE;

DROP INDEX IF EXISTS uq_vault_docs_agent_team_scope_path;
CREATE UNIQUE INDEX uq_vault_docs_agent_team_scope_path
    ON vault_documents (agent_id, COALESCE(team_id, '00000000-0000-0000-0000-000000000000'), scope, path);

DROP TRIGGER IF EXISTS trg_vault_docs_agent_null_scope ON vault_documents;
DROP FUNCTION IF EXISTS vault_docs_agent_null_scope_fix();

DROP INDEX IF EXISTS idx_vault_docs_agent_scope;
CREATE INDEX idx_vault_docs_agent_scope ON vault_documents(agent_id, scope);

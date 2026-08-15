-- Make agent_id nullable so team-scoped and tenant-shared files can exist
-- without an owning agent.

-- 1. Drop NOT NULL on agent_id.
ALTER TABLE vault_documents ALTER COLUMN agent_id DROP NOT NULL;

-- 2. Change FK from CASCADE to SET NULL (agent deletion preserves docs).
ALTER TABLE vault_documents DROP CONSTRAINT vault_documents_agent_id_fkey;
ALTER TABLE vault_documents ADD CONSTRAINT vault_documents_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE SET NULL;

-- 3. Replace unique index: add tenant_id as leading column, COALESCE both nullable cols.
DROP INDEX IF EXISTS uq_vault_docs_agent_team_scope_path;
CREATE UNIQUE INDEX uq_vault_docs_agent_team_scope_path
    ON vault_documents (
        tenant_id,
        COALESCE(agent_id, '00000000-0000-0000-0000-000000000000'),
        COALESCE(team_id, '00000000-0000-0000-0000-000000000000'),
        scope,
        path
    );

-- 4. Trigger: when agent deleted (SET NULL) and no team -> scope='shared'.
CREATE OR REPLACE FUNCTION vault_docs_agent_null_scope_fix()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.agent_id IS NULL AND OLD.agent_id IS NOT NULL AND NEW.team_id IS NULL THEN
        NEW.scope := 'shared';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_vault_docs_agent_null_scope
    BEFORE UPDATE OF agent_id ON vault_documents
    FOR EACH ROW
    EXECUTE FUNCTION vault_docs_agent_null_scope_fix();

-- 5. Partial index for agent-scoped queries (skip NULLs).
DROP INDEX IF EXISTS idx_vault_docs_agent_scope;
CREATE INDEX idx_vault_docs_agent_scope ON vault_documents(agent_id, scope) WHERE agent_id IS NOT NULL;

DROP TRIGGER IF EXISTS trg_vault_docs_team_null_scope ON vault_documents;
DROP FUNCTION IF EXISTS vault_docs_team_null_scope_fix();
DROP INDEX IF EXISTS uq_vault_docs_agent_team_scope_path;
CREATE UNIQUE INDEX IF NOT EXISTS vault_documents_agent_id_scope_path_key
    ON vault_documents(agent_id, scope, path);
DROP INDEX IF EXISTS idx_vault_docs_team;

ALTER TABLE vault_documents DROP COLUMN IF EXISTS team_id;
ALTER TABLE vault_documents DROP COLUMN IF EXISTS custom_scope;
ALTER TABLE vault_links DROP COLUMN IF EXISTS custom_scope;
ALTER TABLE vault_versions DROP COLUMN IF EXISTS custom_scope;
ALTER TABLE memory_documents DROP COLUMN IF EXISTS custom_scope;
ALTER TABLE memory_chunks DROP COLUMN IF EXISTS custom_scope;
ALTER TABLE team_tasks DROP COLUMN IF EXISTS custom_scope;
ALTER TABLE team_task_attachments DROP COLUMN IF EXISTS custom_scope;
ALTER TABLE team_task_comments DROP COLUMN IF EXISTS custom_scope;
ALTER TABLE team_task_events DROP COLUMN IF EXISTS custom_scope;
ALTER TABLE subagent_tasks DROP COLUMN IF EXISTS custom_scope;

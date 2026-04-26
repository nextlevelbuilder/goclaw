DROP INDEX IF EXISTS idx_vault_docs_team_chat;
ALTER TABLE vault_documents DROP COLUMN IF EXISTS chat_id;

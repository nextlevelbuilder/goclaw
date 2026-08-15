-- Reverse Phase 03 schema changes.

DROP INDEX IF EXISTS idx_vault_docs_delegation;

ALTER TABLE vault_links DROP CONSTRAINT IF EXISTS vault_links_to_doc_id_fkey;
ALTER TABLE vault_links
  ADD CONSTRAINT vault_links_to_doc_id_fkey
  FOREIGN KEY (to_doc_id) REFERENCES vault_documents(id);

ALTER TABLE vault_links DROP CONSTRAINT IF EXISTS vault_links_from_doc_id_fkey;
ALTER TABLE vault_links
  ADD CONSTRAINT vault_links_from_doc_id_fkey
  FOREIGN KEY (from_doc_id) REFERENCES vault_documents(id);

DROP INDEX IF EXISTS idx_vault_links_source;
ALTER TABLE vault_links DROP COLUMN IF EXISTS metadata;

DROP INDEX IF EXISTS idx_tta_tenant_basename;
ALTER TABLE team_task_attachments DROP COLUMN IF EXISTS base_name;

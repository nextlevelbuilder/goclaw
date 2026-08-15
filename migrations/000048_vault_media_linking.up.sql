-- Phase 03: vault media linking foundation.
-- Adds base_name index on task attachments, metadata column on vault_links
-- for cleanup tracking, repairs missing CASCADE FKs on vault_links, and a
-- partial index for delegation lookup inside vault_documents.metadata.

-- 1. GENERATED base_name on team_task_attachments (mirrors vault_documents.path_basename).
ALTER TABLE team_task_attachments
  ADD COLUMN IF NOT EXISTS base_name TEXT
  GENERATED ALWAYS AS (lower(regexp_replace(path, '.+/', ''))) STORED;

CREATE INDEX IF NOT EXISTS idx_tta_tenant_basename
  ON team_task_attachments(tenant_id, base_name);

-- 2. metadata column on vault_links for cleanup tracking (task:{id}, delegation:{id}).
ALTER TABLE vault_links
  ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_vault_links_source
  ON vault_links((metadata->>'source'))
  WHERE metadata ? 'source';

-- 3. Fix missing CASCADE on vault_links FKs (SQLite already has CASCADE at schema.sql:1548-1549).
ALTER TABLE vault_links DROP CONSTRAINT IF EXISTS vault_links_from_doc_id_fkey;
ALTER TABLE vault_links
  ADD CONSTRAINT vault_links_from_doc_id_fkey
  FOREIGN KEY (from_doc_id) REFERENCES vault_documents(id) ON DELETE CASCADE;

ALTER TABLE vault_links DROP CONSTRAINT IF EXISTS vault_links_to_doc_id_fkey;
ALTER TABLE vault_links
  ADD CONSTRAINT vault_links_to_doc_id_fkey
  FOREIGN KEY (to_doc_id) REFERENCES vault_documents(id) ON DELETE CASCADE;

-- 4. Partial index for delegation-id lookup in vault_documents.metadata.
--    Keeps index small — only rows explicitly tagged with a delegation_id.
CREATE INDEX IF NOT EXISTS idx_vault_docs_delegation
  ON vault_documents((metadata->>'delegation_id'))
  WHERE metadata ? 'delegation_id';

-- Revert: drop summary column, restore original tsvector (title+path only).
ALTER TABLE vault_documents DROP COLUMN IF EXISTS tsv;
ALTER TABLE vault_documents DROP COLUMN IF EXISTS summary;
ALTER TABLE vault_documents ADD COLUMN tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('simple', coalesce(title,'') || ' ' || coalesce(path,''))) STORED;
CREATE INDEX IF NOT EXISTS idx_vault_docs_tsv ON vault_documents USING gin(tsv);

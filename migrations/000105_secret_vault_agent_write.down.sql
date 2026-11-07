DROP INDEX IF EXISTS idx_secret_vault_entries_pending_agent;

ALTER TABLE secret_vault_entries
    DROP COLUMN IF EXISTS created_by_agent,
    DROP COLUMN IF EXISTS created_via,
    DROP COLUMN IF EXISTS status;

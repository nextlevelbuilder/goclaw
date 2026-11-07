-- Optimistic concurrency + AAD binding for the secret vault. revision is part of
-- the AES-GCM additional data, so every value or metadata write bumps it.
ALTER TABLE secret_vault_entries ADD COLUMN IF NOT EXISTS revision BIGINT NOT NULL DEFAULT 1;

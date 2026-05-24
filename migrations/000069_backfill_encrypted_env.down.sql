-- Reverse of 000069: drop the column added (or re-asserted) by the up step.
-- Safe to run on any DB; no-op when the column is already absent.
ALTER TABLE secure_cli_agent_grants
    DROP COLUMN IF EXISTS encrypted_env;

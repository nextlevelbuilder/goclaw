-- Add optional per-grant env override for secure CLI agent grants.
-- NULL = no grant-level override; binary-level env is used instead.
-- Mirrors secure_cli_user_credentials.encrypted_env AES-256-GCM pattern.
ALTER TABLE secure_cli_agent_grants ADD COLUMN encrypted_env BYTEA;

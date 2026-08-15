-- Credential adapter framework: storage substrate for typed credentials.
-- credential_type carries the cred shape ('env' | 'pat' | 'ssh_key' | future).
-- host_scope binds credentials to a specific hostname (e.g. 'github.com').
-- adapter_name routes the binary to the right CredentialAdapter at exec time.
-- All three columns NULL-by-default to preserve legacy passthrough behavior.

ALTER TABLE secure_cli_user_credentials
    ADD COLUMN IF NOT EXISTS credential_type TEXT NULL,
    ADD COLUMN IF NOT EXISTS host_scope TEXT NULL;

ALTER TABLE secure_cli_binaries
    ADD COLUMN IF NOT EXISTS adapter_name TEXT NULL;

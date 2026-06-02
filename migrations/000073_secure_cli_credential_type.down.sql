ALTER TABLE secure_cli_user_credentials
    DROP COLUMN IF EXISTS credential_type,
    DROP COLUMN IF EXISTS host_scope;

ALTER TABLE secure_cli_binaries
    DROP COLUMN IF EXISTS adapter_name;

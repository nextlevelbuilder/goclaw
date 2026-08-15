-- WARNING: DESTRUCTIVE OPERATION — reads all grant env data before dropping.
-- Running this migration DOWN will permanently discard all per-grant encrypted
-- env override data stored in secure_cli_agent_grants.encrypted_env.
-- Take a logical backup first:
--   pg_dump --table=secure_cli_agent_grants <connstr> > grants_backup.sql
-- See docs/runbooks/packages-migration-rollback.md for full rollback procedure.

DO $$
DECLARE
    row_count bigint;
BEGIN
    -- Only drop if the column exists (idempotent — safe to run twice).
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'secure_cli_agent_grants'
          AND column_name = 'encrypted_env'
    ) THEN
        SELECT COUNT(*) INTO row_count
        FROM secure_cli_agent_grants
        WHERE encrypted_env IS NOT NULL;

        RAISE NOTICE 'DESTRUCTIVE: dropping encrypted_env column; % grant rows have non-null env override data that will be lost', row_count;

        ALTER TABLE secure_cli_agent_grants DROP COLUMN encrypted_env;

        RAISE NOTICE 'encrypted_env column dropped successfully';
    ELSE
        RAISE NOTICE 'encrypted_env column does not exist — migration already reversed, nothing to do';
    END IF;
END $$;

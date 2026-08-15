-- Promote the require_user_credentials flag from settings JSONB to a top-level
-- column so channel factories can filter mcp_servers directly (indexable) and
-- reduce the "read the whole JSONB" cost paid on every message.
--
-- The JSONB entry stays in place for one release cycle for legacy readers
-- (internal/mcp/manager.go:requireUserCreds); a later migration removes it
-- once all callers are on the column.
ALTER TABLE mcp_servers
    ADD COLUMN IF NOT EXISTS require_user_credentials BOOLEAN NOT NULL DEFAULT false;

-- Backfill the column from existing settings blobs so no admin has to
-- re-tick the checkbox after upgrading.
UPDATE mcp_servers
   SET require_user_credentials = COALESCE((settings->>'require_user_credentials')::boolean, false)
 WHERE settings IS NOT NULL
   AND settings ? 'require_user_credentials';

-- Migration 000069: backfill encrypted_env on secure_cli_agent_grants for
-- legacy DBs.
--
-- Why this exists:
--   Upstream's 000058_agent_grants_env_override.up.sql adds an encrypted_env
--   BYTEA column to secure_cli_agent_grants. Synity's fork once shipped a
--   different migration under the same number (000058_bitrix_portals; later
--   renumbered to 000068). DBs that ran the old fork-numbered migration
--   first have schema_migrations.version=58 marked with the bitrix_portals
--   content, so golang-migrate skips upstream's 000058 forever — the column
--   is never added and `secure_cli.list` SQL fails with
--   `column g.encrypted_env does not exist`.
--
-- This migration is idempotent (`IF NOT EXISTS`) so it is a no-op on:
--   - Fresh fork DBs where 000058 (env_override) already ran
--   - Upstream-only DBs where the column was added by 000058
--
-- And it's a real fix on legacy fork DBs where 000058 was the bitrix_portals
-- collision. Once applied everywhere, this file can be deleted in a future
-- cleanup pass (but leaving it costs nothing).

ALTER TABLE secure_cli_agent_grants
    ADD COLUMN IF NOT EXISTS encrypted_env BYTEA;

-- Reverts 000103_secret_vault.up.sql.
--
-- Dropping the tables deletes all stored secrets and the vault audit trail.
-- Back up the database before running this.

DROP TABLE IF EXISTS secret_vault_audit;
DROP TABLE IF EXISTS secret_vault_grants;
DROP TABLE IF EXISTS secret_vault_entries;

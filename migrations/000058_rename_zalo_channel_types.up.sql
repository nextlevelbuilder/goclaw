-- Rename Zalo channel types in channel_instances to align with Zalo's
-- own product taxonomy. Pre-refactor names inverted reality:
--   'zalo_oa'     → static-token Bot variant (actually "zalo_bot")
--   'zalo_oauth'  → phone-tied Official Account via OAuth (the canonical "zalo_oa")
--
-- 'zalo_oauth' was a transient name introduced inside this PR's commit
-- chain and never released. Production DBs only carry the legacy
-- 'zalo_oa' rows (Bot semantics) that must flip to 'zalo_bot'.
--
-- Three-step swap via zalo_oa_tmp sentinel keeps the rename collision-safe
-- even though channel_type has no unique constraint today. golang-migrate's
-- schema_migrations table prevents re-runs, so no idempotency guard is
-- needed (and an EXISTS('zalo_oauth') guard would silently no-op on prod).

UPDATE channel_instances SET channel_type = 'zalo_oa_tmp' WHERE channel_type = 'zalo_oauth';
UPDATE channel_instances SET channel_type = 'zalo_bot'    WHERE channel_type = 'zalo_oa';
UPDATE channel_instances SET channel_type = 'zalo_oa'     WHERE channel_type = 'zalo_oa_tmp';

-- channel_contacts.channel_type stores the same taxonomy and is read by
-- ResolveTenantUserID. Skipping the swap silently loses per-user mappings.
UPDATE channel_contacts SET channel_type = 'zalo_oa_tmp' WHERE channel_type = 'zalo_oauth';
UPDATE channel_contacts SET channel_type = 'zalo_bot'    WHERE channel_type = 'zalo_oa';
UPDATE channel_contacts SET channel_type = 'zalo_oa'     WHERE channel_type = 'zalo_oa_tmp';

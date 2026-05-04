-- Reverse of 000058 up: zalo_oa ↔ zalo_bot only.
-- Up resurrected the transient 'zalo_oauth' name for symmetry, but the
-- runtime allowlists (gateway/methods/channel_instances.go and
-- http/channel_instances.go) reject 'zalo_oauth', so a down rollback that
-- recreates it leaves operators with rows they can't edit.
--
-- ROLLBACK CAVEAT: must run alongside a binary revert. The post-up code
-- treats 'zalo_bot' as Bot semantics; this down restores them to 'zalo_oa'
-- which the new binary rejects. Old binary expects the swapped names back.

UPDATE channel_instances SET channel_type = 'zalo_oa_tmp' WHERE channel_type = 'zalo_oa';
UPDATE channel_instances SET channel_type = 'zalo_oa'     WHERE channel_type = 'zalo_bot';
UPDATE channel_instances SET channel_type = 'zalo_bot'    WHERE channel_type = 'zalo_oa_tmp';

UPDATE channel_contacts SET channel_type = 'zalo_oa_tmp' WHERE channel_type = 'zalo_oa';
UPDATE channel_contacts SET channel_type = 'zalo_oa'     WHERE channel_type = 'zalo_bot';
UPDATE channel_contacts SET channel_type = 'zalo_bot'    WHERE channel_type = 'zalo_oa_tmp';

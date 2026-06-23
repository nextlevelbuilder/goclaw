-- Backfill F1.1: rewrite the legacy mirror-of-synthetic user_id rows to the
-- cross-context person handle ("zalo:<uid>") so Phase 2 auto-merge can
-- GROUP BY user_id to find the same external person across multiple chats.
--
-- Production rolled out PR #1 (per-participant synthetic sender_id) BEFORE
-- the F1 follow-up that started populating user_id with "<connector>:<uid>".
-- Rows written in that window have user_id = sender_id (the synthetic id),
-- which auto-merge can't key on. This migration rewrites those rows in
-- place.
--
-- Idempotent: re-running matches no rows (the WHERE clause requires user_id
-- mirroring sender_id, which is false after the UPDATE).
--
-- Scope: synity_zalo_personal-shape rows only — sender_id starts with
-- "openlines:" and has exactly 3 colons (4 tokens: openlines, instance,
-- chat, uid), and the uid is digits. Other connectors (Facebook, Zalo OA,
-- ...) are not minting synthetic sender_ids today; when they do, that PR
-- will ship its own backfill. This UPDATE will not touch them because the
-- digits-only uid check filters out non-numeric ids.
UPDATE channel_contacts
SET user_id = 'zalo:' || split_part(sender_id, ':', 4)
WHERE channel_type = 'bitrix24'
  AND sender_id LIKE 'openlines:%'
  AND user_id = sender_id
  AND array_length(string_to_array(sender_id, ':'), 1) = 4
  AND split_part(sender_id, ':', 4) ~ '^[0-9]+$';

-- Down migration is a no-op: the up migration rewrites data in-place, and we
-- have no record of which sender_id each user_id was minted from. Rolling
-- back to "user_id = sender_id" for every "zalo:<uid>" row would clobber
-- handles that F1 (the code change, not this migration) populated for new
-- inbound messages after rollout. Leaving the column as-is is safe: the
-- code still operates correctly with either shape — auto-merge keys on
-- user_id regardless of where it was set.
SELECT 1;

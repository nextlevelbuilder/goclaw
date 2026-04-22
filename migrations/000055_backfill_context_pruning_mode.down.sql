-- No-op: cannot distinguish rows backfilled by the up migration from rows
-- where the user explicitly set mode = "cache-ttl". Rollback requires a DB
-- backup taken before the up migration ran.
SELECT 1;

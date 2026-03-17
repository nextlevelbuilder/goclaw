DROP INDEX IF EXISTS idx_sessions_numeric_id;
ALTER TABLE sessions DROP COLUMN IF EXISTS numeric_id;

DROP TABLE IF EXISTS team_user_grants;
DROP INDEX IF EXISTS idx_api_keys_owner_id;
ALTER TABLE api_keys DROP COLUMN IF EXISTS owner_id;
-- Note: handoff_routes and delegation_history are not recreated (dead code removed)

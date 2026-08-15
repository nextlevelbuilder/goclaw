DROP INDEX IF EXISTS idx_team_tasks_user_scope;
ALTER TABLE team_tasks DROP COLUMN IF EXISTS channel;
ALTER TABLE team_tasks DROP COLUMN IF EXISTS user_id;

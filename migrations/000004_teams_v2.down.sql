DROP INDEX IF EXISTS idx_delegation_history_created;
DROP INDEX IF EXISTS idx_delegation_history_team;
DROP INDEX IF EXISTS idx_delegation_history_source;
DROP TABLE IF EXISTS delegation_history;

DROP INDEX IF EXISTS idx_team_tasks_tsv;
ALTER TABLE team_tasks DROP COLUMN IF EXISTS tsv;

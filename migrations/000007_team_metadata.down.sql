ALTER TABLE team_messages DROP COLUMN IF EXISTS task_id;
ALTER TABLE team_messages DROP COLUMN IF EXISTS metadata;
ALTER TABLE team_tasks DROP COLUMN IF EXISTS metadata;
ALTER TABLE delegation_history DROP COLUMN IF EXISTS metadata;
ALTER TABLE handoff_routes DROP COLUMN IF EXISTS metadata;

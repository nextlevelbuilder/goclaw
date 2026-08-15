ALTER TABLE paired_devices DROP COLUMN IF EXISTS expires_at;
ALTER TABLE team_tasks DROP COLUMN IF EXISTS confidence_score;
ALTER TABLE team_messages DROP COLUMN IF EXISTS confidence_score;
ALTER TABLE team_task_comments DROP COLUMN IF EXISTS confidence_score;

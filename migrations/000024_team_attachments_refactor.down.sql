-- Reverse migration: drop new attachments table, count columns, and embedding.
-- Old tables (team_workspace_files, team_messages, etc.) are NOT restored — forward-only.

DROP INDEX IF EXISTS idx_tt_embedding;
ALTER TABLE team_tasks DROP COLUMN IF EXISTS embedding;

DROP TABLE IF EXISTS team_task_attachments;

ALTER TABLE team_tasks DROP COLUMN IF EXISTS comment_count;
ALTER TABLE team_tasks DROP COLUMN IF EXISTS attachment_count;

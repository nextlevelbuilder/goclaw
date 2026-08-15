-- Add comment_type to distinguish note vs blocker comments.
-- Blocker comments trigger task auto-fail + leader escalation.
ALTER TABLE team_task_comments ADD COLUMN IF NOT EXISTS comment_type VARCHAR(20) NOT NULL DEFAULT 'note';

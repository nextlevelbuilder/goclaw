-- Add expiry to paired devices for defense-in-depth.
-- NULL means no expiry (backward compat for existing rows).
ALTER TABLE paired_devices ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

-- Add confidence_score to team tables for agent self-assessment.
ALTER TABLE team_tasks ADD COLUMN IF NOT EXISTS confidence_score FLOAT;
ALTER TABLE team_messages ADD COLUMN IF NOT EXISTS confidence_score FLOAT;
ALTER TABLE team_task_comments ADD COLUMN IF NOT EXISTS confidence_score FLOAT;

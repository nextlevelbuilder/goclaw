-- Add task_id to team_messages for linking messages to tasks
ALTER TABLE team_messages ADD COLUMN IF NOT EXISTS task_id UUID REFERENCES team_tasks(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_team_messages_task ON team_messages(task_id) WHERE task_id IS NOT NULL;

-- Add metadata JSONB to all team-related tables
ALTER TABLE team_messages ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}';
ALTER TABLE team_tasks ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}';
ALTER TABLE delegation_history ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}';
ALTER TABLE handoff_routes ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}';

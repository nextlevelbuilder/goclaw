-- Phase 1: Team attachments refactor — drop workspace_files, messages; path-based attachments
-- Also adds denormalized count columns on team_tasks for dashboard performance.

-- 1. Drop old attachments (FK → team_workspace_files)
DROP TABLE IF EXISTS team_task_attachments;

-- 2. Drop workspace sub-tables (FK → team_workspace_files)
DROP TABLE IF EXISTS team_workspace_comments;
DROP TABLE IF EXISTS team_workspace_file_versions;

-- 3. Drop workspace files table
DROP TABLE IF EXISTS team_workspace_files;

-- 4. Drop team messages table (tool removed)
DROP TABLE IF EXISTS team_messages;

-- 5. Create new path-based attachments table
CREATE TABLE team_task_attachments (
    id                   UUID PRIMARY KEY DEFAULT uuid_generate_v7(),
    task_id              UUID NOT NULL REFERENCES team_tasks(id) ON DELETE CASCADE,
    team_id              UUID NOT NULL REFERENCES agent_teams(id) ON DELETE CASCADE,
    chat_id              VARCHAR(255) NOT NULL DEFAULT '',
    path                 TEXT NOT NULL,
    file_size            BIGINT NOT NULL DEFAULT 0,
    mime_type            VARCHAR(100) DEFAULT '',
    created_by_agent_id  UUID REFERENCES agents(id),
    created_by_sender_id VARCHAR(255) DEFAULT '',
    metadata             JSONB NOT NULL DEFAULT '{}',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(task_id, path)
);
CREATE INDEX idx_tta_task ON team_task_attachments(task_id);
CREATE INDEX idx_tta_team ON team_task_attachments(team_id);

-- 6. Denormalized count columns for dashboard performance
ALTER TABLE team_tasks ADD COLUMN IF NOT EXISTS comment_count INT NOT NULL DEFAULT 0;
ALTER TABLE team_tasks ADD COLUMN IF NOT EXISTS attachment_count INT NOT NULL DEFAULT 0;

-- 7. Vector embedding for semantic task search (subject only)
ALTER TABLE team_tasks ADD COLUMN IF NOT EXISTS embedding vector(1536);
CREATE INDEX IF NOT EXISTS idx_tt_embedding ON team_tasks USING hnsw (embedding vector_cosine_ops);

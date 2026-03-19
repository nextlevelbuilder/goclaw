-- ============================================================
-- Part A: File Writer → Config Permissions merge
-- ============================================================

-- Widen scope column to match group_file_writers.group_id size
ALTER TABLE agent_config_permissions ALTER COLUMN scope TYPE VARCHAR(255);

-- Migrate group_file_writers → agent_config_permissions
INSERT INTO agent_config_permissions (agent_id, scope, config_type, user_id, permission, metadata, created_at)
SELECT agent_id, group_id, 'file_writer', user_id, 'allow',
       jsonb_build_object(
         'displayName', COALESCE(display_name, ''),
         'username', COALESCE(username, '')
       ),
       created_at
FROM group_file_writers
ON CONFLICT (agent_id, scope, config_type, user_id) DO NOTHING;

DROP TABLE group_file_writers;

-- ============================================================
-- Part B: Agent hard delete — FK cascade constraints
-- ============================================================

-- Allow soft-deleted agents to reuse agent_key
ALTER TABLE agents DROP CONSTRAINT agents_agent_key_key;
CREATE UNIQUE INDEX idx_agents_agent_key_active ON agents(agent_key) WHERE deleted_at IS NULL;

-- CASCADE: data belongs to agent, delete with it
ALTER TABLE sessions DROP CONSTRAINT sessions_agent_id_fkey;
ALTER TABLE sessions ADD CONSTRAINT sessions_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE cron_jobs DROP CONSTRAINT cron_jobs_agent_id_fkey;
ALTER TABLE cron_jobs ADD CONSTRAINT cron_jobs_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE heartbeat_run_logs DROP CONSTRAINT heartbeat_run_logs_agent_id_fkey;
ALTER TABLE heartbeat_run_logs ADD CONSTRAINT heartbeat_run_logs_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE agent_teams DROP CONSTRAINT agent_teams_lead_agent_id_fkey;
ALTER TABLE agent_teams ADD CONSTRAINT agent_teams_lead_agent_id_fkey
    FOREIGN KEY (lead_agent_id) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE team_messages DROP CONSTRAINT team_messages_from_agent_id_fkey;
ALTER TABLE team_messages ADD CONSTRAINT team_messages_from_agent_id_fkey
    FOREIGN KEY (from_agent_id) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE delegation_history DROP CONSTRAINT delegation_history_source_agent_id_fkey;
ALTER TABLE delegation_history ADD CONSTRAINT delegation_history_source_agent_id_fkey
    FOREIGN KEY (source_agent_id) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE delegation_history DROP CONSTRAINT delegation_history_target_agent_id_fkey;
ALTER TABLE delegation_history ADD CONSTRAINT delegation_history_target_agent_id_fkey
    FOREIGN KEY (target_agent_id) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE team_workspace_files DROP CONSTRAINT team_workspace_files_uploaded_by_fkey;
ALTER TABLE team_workspace_files ADD CONSTRAINT team_workspace_files_uploaded_by_fkey
    FOREIGN KEY (uploaded_by) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE team_workspace_file_versions DROP CONSTRAINT team_workspace_file_versions_uploaded_by_fkey;
ALTER TABLE team_workspace_file_versions ADD CONSTRAINT team_workspace_file_versions_uploaded_by_fkey
    FOREIGN KEY (uploaded_by) REFERENCES agents(id) ON DELETE CASCADE;

ALTER TABLE team_workspace_comments DROP CONSTRAINT team_workspace_comments_agent_id_fkey;
ALTER TABLE team_workspace_comments ADD CONSTRAINT team_workspace_comments_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE CASCADE;

-- SET NULL: keep row, clear reference
ALTER TABLE cron_run_logs DROP CONSTRAINT cron_run_logs_agent_id_fkey;
ALTER TABLE cron_run_logs ADD CONSTRAINT cron_run_logs_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE SET NULL;

ALTER TABLE team_tasks DROP CONSTRAINT team_tasks_owner_agent_id_fkey;
ALTER TABLE team_tasks ADD CONSTRAINT team_tasks_owner_agent_id_fkey
    FOREIGN KEY (owner_agent_id) REFERENCES agents(id) ON DELETE SET NULL;

ALTER TABLE team_tasks DROP CONSTRAINT team_tasks_created_by_agent_id_fkey;
ALTER TABLE team_tasks ADD CONSTRAINT team_tasks_created_by_agent_id_fkey
    FOREIGN KEY (created_by_agent_id) REFERENCES agents(id) ON DELETE SET NULL;

ALTER TABLE team_messages DROP CONSTRAINT team_messages_to_agent_id_fkey;
ALTER TABLE team_messages ADD CONSTRAINT team_messages_to_agent_id_fkey
    FOREIGN KEY (to_agent_id) REFERENCES agents(id) ON DELETE SET NULL;

ALTER TABLE team_task_comments DROP CONSTRAINT team_task_comments_agent_id_fkey;
ALTER TABLE team_task_comments ADD CONSTRAINT team_task_comments_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE SET NULL;

ALTER TABLE team_task_attachments DROP CONSTRAINT team_task_attachments_added_by_fkey;
ALTER TABLE team_task_attachments ADD CONSTRAINT team_task_attachments_added_by_fkey
    FOREIGN KEY (added_by) REFERENCES agents(id) ON DELETE SET NULL;

-- Clean up previously soft-deleted agents (zombie rows)
-- Placed after CASCADE constraints so related data is auto-cleaned
DELETE FROM agents WHERE deleted_at IS NOT NULL;

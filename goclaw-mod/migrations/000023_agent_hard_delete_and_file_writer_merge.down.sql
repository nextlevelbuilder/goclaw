-- ============================================================
-- Reverse Part B: restore all FK constraints to NO ACTION
-- ============================================================

ALTER TABLE sessions DROP CONSTRAINT sessions_agent_id_fkey;
ALTER TABLE sessions ADD CONSTRAINT sessions_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id);

ALTER TABLE cron_jobs DROP CONSTRAINT cron_jobs_agent_id_fkey;
ALTER TABLE cron_jobs ADD CONSTRAINT cron_jobs_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id);

ALTER TABLE heartbeat_run_logs DROP CONSTRAINT heartbeat_run_logs_agent_id_fkey;
ALTER TABLE heartbeat_run_logs ADD CONSTRAINT heartbeat_run_logs_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id);

ALTER TABLE agent_teams DROP CONSTRAINT agent_teams_lead_agent_id_fkey;
ALTER TABLE agent_teams ADD CONSTRAINT agent_teams_lead_agent_id_fkey
    FOREIGN KEY (lead_agent_id) REFERENCES agents(id);

ALTER TABLE team_messages DROP CONSTRAINT team_messages_from_agent_id_fkey;
ALTER TABLE team_messages ADD CONSTRAINT team_messages_from_agent_id_fkey
    FOREIGN KEY (from_agent_id) REFERENCES agents(id);

ALTER TABLE delegation_history DROP CONSTRAINT delegation_history_source_agent_id_fkey;
ALTER TABLE delegation_history ADD CONSTRAINT delegation_history_source_agent_id_fkey
    FOREIGN KEY (source_agent_id) REFERENCES agents(id);

ALTER TABLE delegation_history DROP CONSTRAINT delegation_history_target_agent_id_fkey;
ALTER TABLE delegation_history ADD CONSTRAINT delegation_history_target_agent_id_fkey
    FOREIGN KEY (target_agent_id) REFERENCES agents(id);

ALTER TABLE team_workspace_files DROP CONSTRAINT team_workspace_files_uploaded_by_fkey;
ALTER TABLE team_workspace_files ADD CONSTRAINT team_workspace_files_uploaded_by_fkey
    FOREIGN KEY (uploaded_by) REFERENCES agents(id);

ALTER TABLE team_workspace_file_versions DROP CONSTRAINT team_workspace_file_versions_uploaded_by_fkey;
ALTER TABLE team_workspace_file_versions ADD CONSTRAINT team_workspace_file_versions_uploaded_by_fkey
    FOREIGN KEY (uploaded_by) REFERENCES agents(id);

ALTER TABLE team_workspace_comments DROP CONSTRAINT team_workspace_comments_agent_id_fkey;
ALTER TABLE team_workspace_comments ADD CONSTRAINT team_workspace_comments_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id);

ALTER TABLE cron_run_logs DROP CONSTRAINT cron_run_logs_agent_id_fkey;
ALTER TABLE cron_run_logs ADD CONSTRAINT cron_run_logs_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id);

ALTER TABLE team_tasks DROP CONSTRAINT team_tasks_owner_agent_id_fkey;
ALTER TABLE team_tasks ADD CONSTRAINT team_tasks_owner_agent_id_fkey
    FOREIGN KEY (owner_agent_id) REFERENCES agents(id);

ALTER TABLE team_tasks DROP CONSTRAINT team_tasks_created_by_agent_id_fkey;
ALTER TABLE team_tasks ADD CONSTRAINT team_tasks_created_by_agent_id_fkey
    FOREIGN KEY (created_by_agent_id) REFERENCES agents(id);

ALTER TABLE team_messages DROP CONSTRAINT team_messages_to_agent_id_fkey;
ALTER TABLE team_messages ADD CONSTRAINT team_messages_to_agent_id_fkey
    FOREIGN KEY (to_agent_id) REFERENCES agents(id);

ALTER TABLE team_task_comments DROP CONSTRAINT team_task_comments_agent_id_fkey;
ALTER TABLE team_task_comments ADD CONSTRAINT team_task_comments_agent_id_fkey
    FOREIGN KEY (agent_id) REFERENCES agents(id);

ALTER TABLE team_task_attachments DROP CONSTRAINT team_task_attachments_added_by_fkey;
ALTER TABLE team_task_attachments ADD CONSTRAINT team_task_attachments_added_by_fkey
    FOREIGN KEY (added_by) REFERENCES agents(id);

-- Restore agent_key unique constraint
DROP INDEX IF EXISTS idx_agents_agent_key_active;
ALTER TABLE agents ADD CONSTRAINT agents_agent_key_key UNIQUE (agent_key);

-- ============================================================
-- Reverse Part A: restore group_file_writers
-- ============================================================

CREATE TABLE group_file_writers (
    agent_id     UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    group_id     VARCHAR(255) NOT NULL,
    user_id      VARCHAR(255) NOT NULL,
    display_name VARCHAR(255),
    username     VARCHAR(255),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (agent_id, group_id, user_id)
);

-- Migrate data back from config permissions
INSERT INTO group_file_writers (agent_id, group_id, user_id, display_name, username, created_at)
SELECT agent_id, scope, user_id,
       metadata->>'displayName', metadata->>'username',
       created_at
FROM agent_config_permissions
WHERE config_type = 'file_writer' AND permission = 'allow';

DELETE FROM agent_config_permissions WHERE config_type = 'file_writer';

-- Restore scope column size
ALTER TABLE agent_config_permissions ALTER COLUMN scope TYPE VARCHAR(100);

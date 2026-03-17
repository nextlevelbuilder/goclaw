/** Team data types matching Go internal/store/team_store.go */

export type EscalationMode = "auto" | "review" | "reject";

export const ESCALATION_ACTIONS = ["pin", "unpin", "tag", "set_template", "delete"] as const;
export type EscalationAction = (typeof ESCALATION_ACTIONS)[number];

export interface TeamNotifyConfig {
  dispatched?: boolean;
  progress?: boolean;
  failed?: boolean;
  mode?: "direct" | "leader";
}

export interface TeamAccessSettings {
  version?: number;
  allow_user_ids?: string[];
  deny_user_ids?: string[];
  allow_channels?: string[];
  deny_channels?: string[];
  notifications?: TeamNotifyConfig;
  escalation_mode?: EscalationMode;
  escalation_actions?: EscalationAction[];
  followup_interval_minutes?: number;
  followup_max_reminders?: number;
  workspace_scope?: string;
  workspace_quota_mb?: number;
}

export interface TeamData {
  id: string;
  name: string;
  lead_agent_id: string;
  lead_agent_key?: string;
  lead_display_name?: string;
  description?: string;
  status: "active" | "archived";
  settings?: Record<string, unknown>;
  created_by: string;
  created_at?: string;
  updated_at?: string;
  member_count?: number;
  members?: TeamMemberData[];
}

export interface TeamMemberData {
  team_id: string;
  agent_id: string;
  agent_key?: string;
  display_name?: string;
  frontmatter?: string;
  emoji?: string;
  role: "lead" | "member" | "reviewer";
  joined_at?: string;
}

export interface TeamWorkspaceFile {
  name: string;
  path: string;
  size: number;
  chat_id: string;
  is_dir?: boolean;
  updated_at?: string;
}

export interface TeamTaskData {
  id: string;
  team_id: string;
  subject: string;
  description?: string;
  status: "pending" | "in_progress" | "completed" | "blocked" | "failed" | "in_review" | "cancelled";
  owner_agent_id?: string;
  owner_agent_key?: string;
  blocked_by?: string[];
  priority: number;
  result?: string;
  user_id?: string;
  created_at?: string;
  updated_at?: string;
  // V2 fields
  task_type?: string;
  task_number?: number;
  identifier?: string;
  created_by_agent_id?: string;
  created_by_agent_key?: string;
  assignee_user_id?: string;
  parent_id?: string;
  chat_id?: string;
  locked_at?: string;
  lock_expires_at?: string;
  progress_percent?: number;
  progress_step?: string;
  // Follow-up reminder fields
  followup_at?: string;
  followup_count?: number;
  followup_max?: number;
  followup_message?: string;
  followup_channel?: string;
  followup_chat_id?: string;
}

export interface TeamTaskComment {
  id: string;
  task_id: string;
  agent_id?: string;
  user_id?: string;
  agent_key?: string;
  content: string;
  created_at: string;
}

export interface TeamTaskEvent {
  id: string;
  task_id: string;
  event_type: string;
  actor_type: "agent" | "human";
  actor_id: string;
  data?: Record<string, unknown>;
  created_at: string;
}

export interface ScopeEntry {
  channel: string;
  chat_id: string;
}

export interface TeamTaskAttachment {
  id: string;
  task_id: string;
  file_id: string;
  added_by?: string;
  file_name?: string;
  created_at: string;
}

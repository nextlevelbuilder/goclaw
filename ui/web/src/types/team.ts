/** Team data types matching Go internal/store/team_store.go */

export interface TeamAccessSettings {
  allow_user_ids?: string[];
  deny_user_ids?: string[];
  allow_channels?: string[];
  deny_channels?: string[];
  progress_notifications?: boolean;
}

export interface TeamData {
  id: string;
  name: string;
  lead_agent_id: string;
  lead_agent_key?: string;
  description?: string;
  status: "active" | "archived";
  settings?: Record<string, unknown>;
  created_by: string;
  created_at?: string;
  updated_at?: string;
}

export interface TeamMemberData {
  team_id: string;
  agent_id: string;
  agent_key?: string;
  display_name?: string;
  frontmatter?: string;
  role: "lead" | "member";
  joined_at?: string;
}

export interface TeamTaskData {
  id: string;
  team_id: string;
  subject: string;
  description?: string;
  status: "pending" | "in_progress" | "completed" | "blocked";
  owner_agent_id?: string;
  owner_agent_key?: string;
  blocked_by?: string[];
  priority: number;
  result?: string;
  created_at?: string;
  updated_at?: string;
}

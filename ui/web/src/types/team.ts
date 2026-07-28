/** Team data types matching Go internal/store/team_store.go */

export type EscalationMode = "auto" | "review" | "reject";

export const ESCALATION_ACTIONS = ["pin", "unpin", "tag", "set_template", "delete"] as const;
export type EscalationAction = (typeof ESCALATION_ACTIONS)[number];

export interface TeamNotifyConfig {
  dispatched?: boolean;
  progress?: boolean;
  failed?: boolean;
  completed?: boolean;
  commented?: boolean;
  new_task?: boolean;
  slow_tool?: boolean;
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
  member_requests?: {
    enabled?: boolean;
    auto_dispatch?: boolean;
  };
  blocker_escalation?: {
    enabled?: boolean;
  };
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
  status: "pending" | "dispatching" | "in_progress" | "completed" | "blocked" | "failed" | "in_review" | "cancelled";
  owner_agent_id?: string;
  owner_agent_key?: string;
  blocked_by?: string[];
  priority: number;
  result?: string;
  user_id?: string;
  channel?: string;
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
  workflow_id?: string;
  workflow_step_id?: string;
  workflow_kind?: "audit" | "work";
  workflow_terminal?: boolean;
  plan_revision?: number;
  // Follow-up reminder fields
  followup_at?: string;
  followup_count?: number;
  followup_max?: number;
  followup_message?: string;
  followup_channel?: string;
  followup_chat_id?: string;
  // Count badges
  comment_count?: number;
  attachment_count?: number;
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

export const WORKFLOW_ACTIONS = [
  "retry_blocked", "request_revision", "apply_replan", "cancel_workflow",
  "fail_workflow", "retry_expansion", "retry_delivery",
] as const;
export type WorkflowAction = (typeof WORKFLOW_ACTIONS)[number];
export type WorkflowActionOutcome = "applied" | "already_applied" | "conflict";

export interface TeamWorkflowDetail {
  id: string;
  team_id: string;
  status: string;
  plan_revision: number;
  coordinator_agent_key: string;
  coordinator_display_name?: string;
  failure_summary?: string;
  result_summary?: string;
  cancel_reason?: string;
  delivery_status: string;
  expansion_attempt_count: number;
  delivery_attempt_count: number;
  last_expansion_error?: string;
  last_delivery_error?: string;
  next_expansion_at?: string;
  next_delivery_at?: string;
  finalized_at?: string;
  delivered_at?: string;
  cancelled_at?: string;
  created_at: string;
  updated_at: string;
}

export interface TeamWorkflowTask {
  id: string;
  task_number?: number;
  subject: string;
  description?: string;
  status: string;
  workflow_step_id: string;
  workflow_kind: string;
  workflow_terminal: boolean;
  plan_revision: number;
  owner_agent_key?: string;
  blocker_reason?: string;
  recovery_count: number;
  dispatch_count: number;
  progress_percent: number;
  progress_step?: string;
  result?: string;
  created_at: string;
  updated_at: string;
}

export interface TeamWorkflowDetailResponse {
  workflow: TeamWorkflowDetail;
  tasks: TeamWorkflowTask[];
  allowed_actions: WorkflowAction[];
}

export interface TeamWorkflowActionRequest {
  teamId: string;
  workflowId: string;
  action: WorkflowAction;
  expectedStatus: string;
  expectedPlanRevision: number;
  taskId?: string;
  expectedTaskStatus?: string;
  reason: string;
}

export interface TeamWorkflowActionResponse extends TeamWorkflowDetailResponse {
  action: WorkflowAction;
  outcome: WorkflowActionOutcome;
}

export interface TeamTaskAttachment {
  id: string;
  task_id: string;
  team_id: string;
  chat_id?: string;
  path: string;
  file_size: number;
  mime_type?: string;
  created_by_agent_id?: string;
  created_by_sender_id?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
  download_url?: string;
}

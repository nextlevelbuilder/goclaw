/** TypeScript interfaces mirroring Go pkg/protocol/team_events.go */

// --- Delegation events ---

export interface DelegationEventPayload {
  delegation_id: string;
  source_agent_id: string;
  source_agent_key: string;
  source_display_name?: string;
  target_agent_id: string;
  target_agent_key: string;
  target_display_name?: string;
  user_id: string;
  channel: string;
  chat_id: string;
  mode: string;
  task?: string;
  team_id?: string;
  team_task_id?: string;
  status?: string;
  elapsed_ms?: number;
  error?: string;
  created_at: string;
}

export interface DelegationProgressItem {
  delegation_id: string;
  target_agent_key: string;
  target_display_name?: string;
  elapsed_ms: number;
  team_task_id?: string;
}

export interface DelegationProgressPayload {
  source_agent_id: string;
  source_agent_key: string;
  user_id: string;
  channel: string;
  chat_id: string;
  team_id?: string;
  active_delegations: DelegationProgressItem[];
}

export interface DelegationAccumulatedPayload {
  delegation_id: string;
  source_agent_id: string;
  source_agent_key: string;
  target_agent_key: string;
  target_display_name?: string;
  user_id: string;
  channel: string;
  chat_id: string;
  team_id?: string;
  team_task_id?: string;
  siblings_remaining: number;
  elapsed_ms?: number;
}

export interface DelegationAnnounceResultSummary {
  agent_key: string;
  display_name?: string;
  has_media: boolean;
  content_preview?: string;
}

export interface DelegationAnnouncePayload {
  source_agent_id: string;
  source_agent_key: string;
  source_display_name?: string;
  user_id: string;
  channel: string;
  chat_id: string;
  team_id?: string;
  results: DelegationAnnounceResultSummary[];
  completed_task_ids?: string[];
  total_elapsed_ms: number;
  has_media: boolean;
}

export interface QualityGateRetryPayload {
  delegation_id: string;
  target_agent_key: string;
  user_id: string;
  channel: string;
  chat_id: string;
  team_id?: string;
  team_task_id?: string;
  gate_type: string;
  attempt: number;
  max_retries: number;
  feedback?: string;
}

// --- Team task events ---

export interface TeamTaskEventPayload {
  team_id: string;
  task_id: string;
  subject?: string;
  status: string;
  owner_agent_key?: string;
  owner_display_name?: string;
  reason?: string;
  user_id: string;
  channel: string;
  chat_id: string;
  timestamp: string;
}

// --- Team message events ---

export interface TeamMessageEventPayload {
  team_id: string;
  from_agent_key: string;
  from_display_name?: string;
  to_agent_key: string;
  to_display_name?: string;
  message_type: string;
  preview: string;
  task_id?: string;
  user_id: string;
  channel: string;
  chat_id: string;
}

// --- Team CRUD events ---

export interface TeamCreatedPayload {
  team_id: string;
  team_name: string;
  lead_agent_key: string;
  lead_display_name?: string;
  member_count: number;
}

export interface TeamUpdatedPayload {
  team_id: string;
  team_name: string;
  changes: string[];
}

export interface TeamDeletedPayload {
  team_id: string;
  team_name: string;
}

export interface TeamMemberAddedPayload {
  team_id: string;
  team_name: string;
  agent_id: string;
  agent_key: string;
  display_name?: string;
  role: string;
}

export interface TeamMemberRemovedPayload {
  team_id: string;
  team_name: string;
  agent_id: string;
  agent_key: string;
  display_name?: string;
}

// --- Agent link events ---

export interface AgentLinkCreatedPayload {
  link_id: string;
  source_agent_id: string;
  source_agent_key: string;
  target_agent_id: string;
  target_agent_key: string;
  direction: string;
  team_id?: string;
  status: string;
}

export interface AgentLinkUpdatedPayload {
  link_id: string;
  source_agent_key: string;
  target_agent_key: string;
  direction?: string;
  status?: string;
  changes: string[];
}

export interface AgentLinkDeletedPayload {
  link_id: string;
  source_agent_key: string;
  target_agent_key: string;
}

// --- Enriched agent events (from internal/agent/loop_types.go) ---

export interface EnrichedAgentEventPayload {
  type: string;
  agentId: string;
  runId: string;
  payload?: {
    content?: string;
    name?: string;
    id?: string;
    is_error?: boolean;
    error?: string;
  };
  delegationId?: string;
  teamId?: string;
  teamTaskId?: string;
  parentAgentId?: string;
  userId?: string;
  channel?: string;
  chatId?: string;
}

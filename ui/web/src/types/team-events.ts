/** TypeScript interfaces mirroring Go pkg/protocol/team_events.go */

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
  task_number?: number;
  comment_text?: string;
  progress_percent?: number;
  progress_step?: string;
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
  runKind?: string; // "delegation", "announce" — omitted for user-initiated runs
  payload?: {
    content?: string;
    message?: string;
    name?: string;
    id?: string;
    is_error?: boolean;
    error?: string;
    arguments?: Record<string, unknown>;
  };
  delegationId?: string;
  teamId?: string;
  teamTaskId?: string;
  parentAgentId?: string;
  userId?: string;
  channel?: string;
  chatId?: string;
}

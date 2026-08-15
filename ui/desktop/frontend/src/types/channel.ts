// Channel instance data returned by GET /v1/channels/instances
export interface ChannelInstanceData {
  id: string;
  name: string;
  display_name: string;
  channel_type: string; // "telegram" | "discord"
  agent_id: string;
  credentials: Record<string, string>;
  config: Record<string, unknown>;
  enabled: boolean;
  created_by: string;
  created_at: string;
  updated_at: string;
}

// Input for creating/updating a channel instance
export interface ChannelInstanceInput {
  name: string;
  displayName: string;
  channelType: string;
  agentId: string;
  credentials: Record<string, string>;
  config: Record<string, unknown>;
  enabled: boolean;
}

// Live channel status from WS channels.status
export interface ChannelStatus {
  enabled: boolean;
  running: boolean;
  state?:
    | "registered"
    | "starting"
    | "healthy"
    | "degraded"
    | "failed"
    | "stopped";
  summary?: string;
  detail?: string;
  failure_kind?: "auth" | "config" | "network" | "unknown";
  retryable?: boolean;
  checked_at?: string;
  failure_count?: number;
  consecutive_failures?: number;
  first_failed_at?: string;
  last_failed_at?: string;
  last_healthy_at?: string;
  remediation?: {
    code: "reauth" | "open_credentials" | "open_advanced" | "check_network";
    headline: string;
    hint?: string;
    target?: "credentials" | "advanced" | "reauth" | "details";
  };
}

// Pending pairing request from WS device.pair.list
export interface PendingPairing {
  code: string;
  sender_id: string;
  channel: string;
  chat_id: string;
  account_id: string;
  created_at: number;
  expires_at: number;
}

// Approved paired device from WS device.pair.list
export interface PairedDevice {
  sender_id: string;
  channel: string;
  chat_id: string;
  paired_at: number;
  paired_by: string;
}

// Manager group info from GET /v1/channels/instances/{id}/writers/groups
export interface GroupManagerGroupInfo {
  group_id: string;
  writer_count: number;
}

// Manager data from GET /v1/channels/instances/{id}/writers
export interface GroupManagerData {
  user_id: string;
  display_name?: string;
  username?: string;
}

// Contact from GET /v1/contacts
export interface ChannelContact {
  id: string;
  sender_id: string;
  display_name?: string;
  username?: string;
  channel_type: string;
}

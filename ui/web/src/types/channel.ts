export interface ChannelInstanceData {
  id: string;
  name: string;
  display_name: string;
  channel_type: string;
  agent_id: string;
  config: Record<string, unknown> | null;
  enabled: boolean;
  is_default: boolean;
  has_credentials: boolean;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface ChannelRuntimeStatus {
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

export interface ChannelInstanceInput {
  name: string;
  display_name?: string;
  channel_type: string;
  agent_id: string;
  credentials?: Record<string, unknown>;
  config?: Record<string, unknown>;
  enabled?: boolean;
}

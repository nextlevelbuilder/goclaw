export interface MCPServerData {
  id: string;
  name: string;
  display_name: string;
  transport: "stdio" | "sse" | "streamable-http";
  command: string;
  args: string[] | null;
  url: string;
  headers: Record<string, string> | null;
  env: Record<string, string> | null;
  tool_prefix: string;
  timeout_sec: number;
  settings?: { require_user_credentials?: boolean };
  enabled: boolean;
  created_by: string;
  agent_count?: number;
  created_at: string;
  updated_at: string;
}

export interface MCPServerInput {
  name: string;
  display_name?: string;
  transport: string;
  command?: string;
  args?: string[];
  url?: string;
  headers?: Record<string, string>;
  env?: Record<string, string>;
  tool_prefix?: string;
  timeout_sec?: number;
  settings?: { require_user_credentials?: boolean };
  enabled?: boolean;
}

export interface MCPToolInfo {
  name: string;
  description?: string;
}

export interface MCPAgentGrant {
  id: string;
  server_id: string;
  agent_id: string;
  enabled: boolean;
  tool_allow: string[] | null;
  tool_deny: string[] | null;
  granted_by: string;
  created_at: string;
}

export interface MCPUserCredentialStatus {
  has_credentials: boolean;
  has_api_key: boolean;
  has_headers: boolean;
  has_env: boolean;
}

export interface MCPUserCredentialInput {
  api_key?: string;
  headers?: Record<string, string>;
  env?: Record<string, string>;
}

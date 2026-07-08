export interface MCPOAuthSettings {
  auth_type?: "oauth" | "";
  use_dcr?: boolean;
  grant_type?: "pkce" | "authorization_code" | "client_credentials";
  auth_endpoint?: string;
  token_endpoint?: string;
  client_id?: string;
  /** Write-only — never returned by the API (stored encrypted). */
  client_secret?: string;
  scope?: string;
}

export interface MCPServerSettings {
  require_user_credentials?: boolean;
  tool_hints?: {
    global?: string;
    tools?: Record<string, string>;
  };
  oauth?: MCPOAuthSettings;
}

export interface MCPOAuthStatus {
  has_token: boolean;
  client_id?: string;
  issuer?: string;
  expires_at?: string;
  expired?: boolean;
}

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
  settings?: MCPServerSettings;
  enabled: boolean;
  /**
   * True when the server mints credentials per-user at message time
   * (Bitrix24 auto-onboard etc.). Promoted from `settings.require_user_credentials`
   * to a top-level column; the settings entry stays for one release cycle
   * so legacy readers keep working. Prefer this field over the settings
   * entry when both are present.
   */
  require_user_credentials?: boolean;
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
  settings?: MCPServerSettings;
  enabled?: boolean;
  /**
   * Top-level twin of `settings.require_user_credentials`. Send both while
   * the server backend is still on the migration window (readers may hit
   * either field); after Phase 5 lands the settings entry disappears.
   */
  require_user_credentials?: boolean;
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

export type SecretKind = "api_key" | "login";

/** "pending" entries were saved by an agent and stay unusable until approved. */
export type SecretVaultStatus = "active" | "pending";

/** Secret metadata. The API never returns the secret value. */
export interface SecretVaultEntry {
  id: string;
  name: string;
  kind: SecretKind;
  allowed_domains: string[];
  require_approval: boolean;
  hints: string;
  /** Version of the entry; sent back as If-Match so a stale edit is refused. */
  revision: number;
  created_by: string;
  status: SecretVaultStatus;
  created_via: "user" | "agent";
  /** Agent that saved the entry; null for user entries or after the agent is deleted. */
  created_by_agent: string | null;
  created_at: string;
  updated_at: string;
}

/** Body of POST /v1/secret-vault/{id}/approve: the domains the user confirmed. */
export interface SecretVaultApproveInput {
  allowed_domains: string[];
}

/** Write-only: sent once on create, never read back. */
export interface SecretVaultCreateInput {
  name: string;
  kind: SecretKind;
  allowed_domains: string[];
  require_approval: boolean;
  hints: string;
  username?: string;
  secret: string;
}

export interface SecretVaultMetaInput {
  allowed_domains?: string[];
  require_approval?: boolean;
  hints?: string;
}

export interface SecretVaultValueInput {
  username?: string;
  secret: string;
}

export interface SecretVaultAuditRow {
  id: string;
  entry_name: string;
  agent_id?: string;
  user_id: string;
  action: string;
  target_host: string;
  outcome: string;
  created_at: string;
}

/** Payload of the secretVault.form.requested event. */
export interface SecretRequest {
  id: string;
  /** "api_key" | "login" for a form, "approval" to approve a stored secret. */
  kind: "api_key" | "login" | "approval";
  name: string;
  domains?: string[];
  reason?: string;
  hints?: string;
  host?: string;
  action?: string;
  agentKey?: string;
  sessionKey?: string;
  userId?: string;
  expiresAt: number;
}

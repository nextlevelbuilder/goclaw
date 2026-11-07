export type SecretKind = "api_key" | "login";

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
  created_at: string;
  updated_at: string;
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

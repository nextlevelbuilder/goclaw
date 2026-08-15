export type CLIEnvEntryKind = "sensitive" | "value";

export interface CLIEnvEntryInput {
  kind: CLIEnvEntryKind;
  value: string;
}

export interface CLIEnvEntryResponse {
  kind: CLIEnvEntryKind;
  value: string | null;
  masked: boolean;
}

export type CLIEnvPayload = Record<string, string | CLIEnvEntryInput>;
export type CLIGitCredentialType = "env" | "pat" | "ssh_key";

export interface SecureCLIBinary {
  id: string;
  binary_name: string;
  binary_path?: string;
  description: string;
  deny_args: string[];
  deny_verbose: string[];
  timeout_seconds: number;
  tips: string;
  is_global: boolean;
  enabled: boolean;
  created_by: string;
  created_at: string;
  updated_at: string;
  /** Env variable names only (no values); from API for edit form */
  env_keys?: string[];
  /** Sanitized env metadata; sensitive values are masked, value entries include value. */
  env?: Record<string, CLIEnvEntryResponse>;
  /**
   * Agent grants summary for row chips (Phase 4 API field).
   * Absent on older API versions — capability-probe: skip rendering if undefined.
   */
  agent_grants_summary?: AgentGrantSummary[];
  /**
   * Adapter name routes per-user credentials through a typed flow.
   * Phase 5: "git" → PAT/SSH form fields; absent/empty → legacy env-vars form.
   */
  adapter_name?: string;
}

export interface CLIPresetEnvVar {
  name: string;
  desc: string;
  is_file?: boolean;
  optional?: boolean;
}

export interface CLIPreset {
  binary_name: string;
  description: string;
  env_vars: CLIPresetEnvVar[];
  deny_args: string[];
  deny_verbose: string[];
  timeout: number;
  tips: string;
  adapter_name?: string;
}

export interface CLICredentialInput {
  preset?: string;
  binary_name: string;
  binary_path?: string;
  description?: string;
  deny_args?: string[];
  deny_verbose?: string[];
  timeout_seconds?: number;
  tips?: string;
  is_global?: boolean;
  enabled?: boolean;
  env?: CLIEnvPayload;
}

/** Per-agent grant with optional setting overrides */
export interface CLIAgentGrant {
  id: string;
  binary_id: string;
  agent_id: string;
  deny_args: string[] | null;
  deny_verbose: string[] | null;
  timeout_seconds: number | null;
  tips: string | null;
  enabled: boolean;
  /** Whether this grant has an env override (keys present, values encrypted) */
  env_set?: boolean;
  /** Env variable names only (no values); populated when env_set=true */
  env_keys?: string[];
  /** Sanitized env metadata; sensitive values are masked, value entries include value. */
  env?: Record<string, CLIEnvEntryResponse>;
  created_at: string;
  updated_at: string;
}

export interface CLIAgentGrantInput {
  agent_id: string;
  deny_args?: string[] | null;
  deny_verbose?: string[] | null;
  timeout_seconds?: number | null;
  tips?: string | null;
  enabled?: boolean;
  /**
   * env_vars semantics — 3-state, all three distinct behaviors:
   *
   * - **absent / undefined** → keep existing env override (omit from request payload)
   * - **null**               → clear override; grant falls back to binary-level defaults
   * - **`{}` (empty map)**   → treated as clear (same as null) — wipes the override
   * - **`{K: V, ...}`**      → replace the entire env override with this map
   *
   * Backend: internal/http/secure_cli_agent_grants.go handleUpdate (3-state env_vars branch).
   * Keys must match ^[A-Z_][A-Z0-9_]*$ and must not be on the denylist.
   */
  env_vars?: CLIEnvPayload | null;
}

export interface CLIAgentCredential {
  id: string;
  binary_id: string;
  agent_id: string;
  agent_key?: string;
  name?: string;
  has_secret: boolean;
  env_keys?: string[];
  env?: Record<string, CLIEnvEntryResponse>;
  credential_type?: CLIGitCredentialType | string | null;
  host_scope?: string | null;
  created_by?: string;
  created_at: string;
  updated_at: string;
}

export interface CLIAgentCredentialInput {
  env?: CLIEnvPayload;
  credential_type?: Exclude<CLIGitCredentialType, "env">;
  host_scope?: string;
  blob?: Record<string, string>;
}

/** Summary of a single grant shown in the table row chips (Phase 4 API field). */
export interface AgentGrantSummary {
  grant_id: string;
  agent_id: string;
  agent_key: string;
  name: string;
  enabled: boolean;
  env_set: boolean;
}

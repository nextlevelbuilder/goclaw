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
  env?: Record<string, string>;
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
}

/** Agent data types matching Go internal/store/agent_store.go */

// --- Per-agent config types (matching Go config structs) ---

export interface ToolPolicyConfig {
  profile?: string; // "full", "coding", "messaging", "minimal"
  allow?: string[];
  deny?: string[];
  alsoAllow?: string[];
  byProvider?: Record<string, { profile?: string; allow?: string[]; deny?: string[]; alsoAllow?: string[] }>;
}

export interface SubagentsConfig {
  maxConcurrent?: number;
  maxSpawnDepth?: number;
  maxChildrenPerAgent?: number;
  archiveAfterMinutes?: number;
  model?: string;
}

export interface CompactionConfig {
  reserveTokensFloor?: number;
  maxHistoryShare?: number;
  minMessages?: number;
  keepLastMessages?: number;
  memoryFlush?: {
    enabled?: boolean;
    softThresholdTokens?: number;
  };
}

export interface ContextPruningConfig {
  mode?: "off" | "cache-ttl";
  keepLastAssistants?: number;
  softTrimRatio?: number;
  hardClearRatio?: number;
  minPrunableToolChars?: number;
  softTrim?: {
    maxChars?: number;
    headChars?: number;
    tailChars?: number;
  };
  hardClear?: {
    enabled?: boolean;
    placeholder?: string;
  };
}

export interface SandboxConfig {
  mode?: "off" | "non-main" | "all";
  image?: string;
  workspace_access?: "none" | "ro" | "rw";
  scope?: "session" | "agent" | "shared";
  memory_mb?: number;
  cpus?: number;
  timeout_sec?: number;
  network_enabled?: boolean;
}

export interface MemoryConfig {
  enabled?: boolean;
  embedding_provider?: string;
  embedding_model?: string;
  max_results?: number;
  max_chunk_len?: number;
  vector_weight?: number;
  text_weight?: number;
}

export interface WorkspaceSharingConfig {
  shared_dm?: boolean;
  shared_group?: boolean;
  shared_users?: string[];
  share_memory?: boolean;
}

export interface AgentData {
  id: string;
  agent_key: string;
  display_name?: string;
  frontmatter?: string;
  owner_id: string;
  provider: string;
  model: string;
  context_window: number;
  max_tool_iterations: number;
  workspace: string;
  restrict_to_workspace: boolean;
  agent_type: "open" | "predefined";
  is_default: boolean;
  status: string;
  created_at?: string;
  updated_at?: string;

  // Per-agent JSONB configs (null/undefined = use global defaults)
  tools_config?: ToolPolicyConfig | null;
  sandbox_config?: SandboxConfig | null;
  subagents_config?: SubagentsConfig | null;
  memory_config?: MemoryConfig | null;
  compaction_config?: CompactionConfig | null;
  context_pruning?: ContextPruningConfig | null;
  other_config?: Record<string, unknown> | null;
  budget_monthly_cents?: number | null;
}

export interface AgentShareData {
  id: string;
  agent_id: string;
  user_id: string;
  role: string;
  granted_by: string;
  created_at?: string;
}

export interface AgentLinkSettings {
  require_role?: string;
  user_allow?: string[];
  user_deny?: string[];
}

export interface AgentLinkData {
  id: string;
  source_agent_id: string;
  target_agent_id: string;
  direction: "outbound" | "inbound" | "bidirectional";
  team_id?: string;
  team_name?: string;
  description?: string;
  max_concurrent: number;
  settings?: AgentLinkSettings;
  status: "active" | "disabled";
  created_by: string;
  created_at?: string;
  updated_at?: string;
  source_agent_key?: string;
  target_agent_key?: string;
  target_display_name?: string;
  target_description?: string;
}

export interface AgentInfo {
  id: string;
  name: string;
  model?: string;
  provider?: string;
  isRunning?: boolean;
  emoji?: string;
  avatar?: string;
  description?: string;
  workspace?: string;
  agentType?: "open" | "predefined";
  status?: string;
}

export interface AgentIdentity {
  agentId: string;
  name: string;
  emoji?: string;
  avatar?: string;
  description?: string;
}

export interface BootstrapFile {
  name: string;
  missing: boolean;
  size: number;
  content?: string;
  path?: string;
  updatedAtMs?: number;
}

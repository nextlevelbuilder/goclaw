/** Agent data types matching Go internal/store/agent_store.go + web UI types */

// --- Per-agent config types (matching Go config structs) ---

export interface MemoryConfig {
  enabled?: boolean
  embedding_provider?: string
  embedding_model?: string
  max_results?: number
  max_chunk_len?: number
  chunk_overlap?: number
  vector_weight?: number
  text_weight?: number
  min_score?: number
}

export interface CompactionConfig {
  reserveTokensFloor?: number
  maxHistoryShare?: number
  keepLastMessages?: number
  memoryFlush?: {
    enabled?: boolean
    softThresholdTokens?: number
  }
}

export interface ContextPruningConfig {
  mode?: 'off' | 'cache-ttl'
  keepLastAssistants?: number
  softTrimRatio?: number
  hardClearRatio?: number
  softTrim?: { maxChars?: number; headChars?: number; tailChars?: number }
  hardClear?: { enabled?: boolean }
}

export interface SubagentsConfig {
  maxConcurrent?: number
  maxSpawnDepth?: number
  maxChildrenPerAgent?: number
  archiveAfterMinutes?: number
  model?: string
}

export interface ToolPolicyConfig {
  profile?: string
  allow?: string[]
  deny?: string[]
  alsoAllow?: string[]
  toolCallPrefix?: string
}

export interface SandboxConfig {
  mode?: 'off' | 'non-main' | 'all'
  image?: string
  workspace_access?: 'none' | 'ro' | 'rw'
  scope?: 'session' | 'agent' | 'shared'
  timeout_sec?: number
  memory_mb?: number
  cpus?: number
  network_enabled?: boolean
}

export type ReasoningOverrideMode = 'inherit' | 'custom'

export interface AgentReasoningConfig {
  override_mode?: ReasoningOverrideMode
  effort?: string
  fallback?: 'downgrade' | 'provider_default' | 'off'
}

// --- Main agent data ---

export interface AgentData {
  id: string
  agent_key: string
  display_name?: string
  frontmatter?: string
  owner_id: string
  provider: string
  model: string
  context_window: number
  max_tool_iterations: number
  workspace: string
  restrict_to_workspace: boolean
  agent_type: 'open' | 'predefined'
  is_default: boolean
  status: string // "active" | "summoning" | "summon_failed" | "idle" | "running"
  created_at?: string
  updated_at?: string

  // Per-agent JSONB configs (null/undefined = use global defaults)
  memory_config?: MemoryConfig | null
  compaction_config?: CompactionConfig | null
  context_pruning?: ContextPruningConfig | null
  tools_config?: ToolPolicyConfig | null
  sandbox_config?: SandboxConfig | null
  subagents_config?: SubagentsConfig | null
  other_config?: Record<string, unknown> | null
  tenant_id?: string
}

export interface AgentInput {
  agent_key: string
  display_name?: string
  provider: string
  model: string
  agent_type: 'open' | 'predefined'
  is_default?: boolean
  context_window?: number
  max_tool_iterations?: number
  memory_config?: MemoryConfig | null
  other_config?: Record<string, unknown>
}

/** Bootstrap file info from agents.files.list / agents.files.get */
export interface BootstrapFile {
  name: string
  missing: boolean
  size?: number
  content?: string
}

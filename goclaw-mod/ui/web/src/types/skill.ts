export interface SkillInfo {
  id?: string;
  name: string;
  slug?: string;
  description: string;
  source: string;
  visibility?: string;
  tags?: string[];
  version?: number;
  is_system?: boolean;
  status?: string;
  enabled?: boolean;
  tenant_enabled?: boolean | null;
  author?: string;
  creator_agent?: SkillAgentRef;
  manager_agents?: SkillAgentRef[];
  missing_deps?: string[];
}

export interface SkillAgentRef {
  id?: string;
  agent_key?: string;
  display_name?: string;
}

export interface SkillFile {
  path: string;
  name: string;
  isDir: boolean;
  size: number;
}

export interface SkillVersions {
  versions: number[];
  current: number;
}

export interface SkillWithGrant {
  id: string;
  name: string;
  slug: string;
  description: string;
  visibility: string;
  version: number;
  granted: boolean;
  can_manage?: boolean;
  pinned_version?: number;
  is_system: boolean;
}

export interface SkillAgentGrant {
  agent_id: string;
  agent_key?: string;
  display_name?: string;
  pinned_version: number;
  granted_by: string;
  can_manage: boolean;
}

export interface SkillEvolutionSettings {
  tenant_id: string;
  skill_id: string;
  enabled: boolean;
  mode: "suggest_only" | "auto_analyze";
  last_analyzed_at?: string;
}

export interface SkillFailureReason {
  reason: string;
  count: number;
  last_seen: string;
}

export interface SkillUsageStats {
  skill_id: string;
  total_calls: number;
  started: number;
  succeeded: number;
  failed: number;
  abandoned: number;
  success_rate: number;
  failure_rate: number;
  last_used_at?: string;
  top_failure_reasons?: SkillFailureReason[];
}

export interface SkillImprovementSuggestion {
  id: string;
  skill_id: string;
  skill_slug: string;
  suggestion_type: string;
  status: "pending" | "approved" | "rejected" | "applied";
  reason: string;
  target_file?: string;
  applied_version?: number;
  created_at: string;
  updated_at: string;
}

export interface SkillActivityLog {
  id: string;
  actor_type: string;
  actor_id: string;
  action: string;
  entity_type?: string;
  entity_id?: string;
  details?: unknown;
  created_at: string;
}

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

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
  author?: string;
  missing_deps?: string[];
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
  pinned_version?: number;
  is_system: boolean;
}

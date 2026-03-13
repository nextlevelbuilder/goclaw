export interface ManagedToolInfo {
  id: string;
  name: string;
  slug: string;
  description: string;
  visibility: string;
  version: number;
  status: string;
  enabled: boolean;
  is_system?: boolean;
  runtime?: string;
  entry_point?: string;
  tags?: string[];
  owner_id?: string;
}

export interface ManagedToolFile {
  path: string;
  name: string;
  isDir: boolean;
  size: number;
}

export interface ManagedToolVersions {
  versions: number[];
  current: number;
}

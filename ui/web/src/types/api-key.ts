export interface ApiKeyData {
  id: string;
  name: string;
  prefix: string;
  scopes: string[];
  expires_at: string | null;
  last_used_at: string | null;
  revoked: boolean;
  created_by: string;
  created_at: string;
  updated_at: string;
  tenant_id?: string;
}

export interface ApiKeyCreateInput {
  name: string;
  scopes: string[];
  expires_in?: number; // seconds; undefined = never
  tenant_id?: string;  // cross-tenant admin only; omit for system-wide key
}

export interface ApiKeyCreateResponse extends ApiKeyData {
  key: string; // raw key, shown only once
}

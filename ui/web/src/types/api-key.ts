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
}

export interface ApiKeyCreateInput {
  name: string;
  scopes: string[];
  expires_in?: number; // seconds; undefined = never
}

export interface ApiKeyCreateResponse extends ApiKeyData {
  key: string; // raw key, shown only once
}

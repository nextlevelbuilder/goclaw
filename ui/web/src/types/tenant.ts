export interface TenantMembership {
  id: string
  name: string
  slug: string
  role: string
  status: string
}

export interface TenantData {
  id: string
  name: string
  slug: string
  status: string
  settings?: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface TenantUserData {
  id: string
  tenant_id: string
  user_id: string
  display_name?: string
  role: string
  metadata?: Record<string, unknown>
  created_at: string
  updated_at: string
}

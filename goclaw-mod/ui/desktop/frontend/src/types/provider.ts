export interface ProviderData {
  id: string
  name: string
  display_name?: string
  provider_type: string
  api_base?: string
  api_key?: string
  enabled: boolean
  settings?: Record<string, unknown>
  created_at?: string
  updated_at?: string
}

export interface ProviderInput {
  name: string
  display_name?: string
  provider_type: string
  api_base?: string
  api_key?: string
  enabled?: boolean
  settings?: Record<string, unknown>
}

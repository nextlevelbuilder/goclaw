export interface BuiltinToolData {
  name: string
  display_name: string
  description: string
  category: string
  enabled: boolean
  settings: Record<string, unknown>
  requires: string[] | null
  metadata: Record<string, unknown>
  created_at: string
  updated_at: string
}

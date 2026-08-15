export interface SkillInfo {
  id?: string
  name: string
  slug?: string
  description: string
  source: string
  visibility?: string
  tags?: string[] | null
  version?: number
  is_system?: boolean
  status?: string      // "active" | "archived"
  enabled?: boolean
  author?: string
  missing_deps?: string[] | null
}

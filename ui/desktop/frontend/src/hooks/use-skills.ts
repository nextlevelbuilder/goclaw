import { useState, useEffect, useCallback } from 'react'
import { getApiClient, isApiClientReady } from '../lib/api'
import { toast } from '../stores/toast-store'
import type { SkillInfo } from '../types/skill'

export const MAX_SKILLS_LITE = 10

export interface RuntimeInfo {
  name: string
  available: boolean
  version?: string
}

export interface RuntimeStatus {
  runtimes: RuntimeInfo[]
  ready: boolean
}

export interface UploadResult {
  id: string
  slug: string
  version: number
  name: string
  deps_warning?: string
}

export function useSkills() {
  const [skills, setSkills] = useState<SkillInfo[]>([])
  const [loading, setLoading] = useState(true)

  const fetchSkills = useCallback(async () => {
    if (!isApiClientReady()) { setLoading(false); return }
    try {
      const res = await getApiClient().get<{ skills: SkillInfo[] | null }>('/v1/skills')
      setSkills(res.skills ?? [])
    } catch (err) {
      console.error('Failed to fetch skills:', err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchSkills() }, [fetchSkills])

  const toggleSkill = useCallback(async (id: string, enabled: boolean) => {
    setSkills((prev) => prev.map((s) => s.id === id ? { ...s, enabled } : s))
    try {
      await getApiClient().post(`/v1/skills/${encodeURIComponent(id)}/toggle`, { enabled })
    } catch (err) {
      console.error('Failed to toggle skill:', err)
      setSkills((prev) => prev.map((s) => s.id === id ? { ...s, enabled: !enabled } : s))
    }
  }, [])

  const uploadSkill = useCallback(async (file: File): Promise<UploadResult> => {
    try {
      const res = await getApiClient().uploadFile<UploadResult>('/v1/skills/upload', file)
      await fetchSkills()
      toast.success('Skill uploaded')
      return res
    } catch (err) {
      toast.error('Failed to upload skill', (err as Error).message)
      throw err
    }
  }, [fetchSkills])

  const checkRuntimes = useCallback(async (): Promise<RuntimeStatus | null> => {
    if (!isApiClientReady()) return null
    return getApiClient().get<RuntimeStatus>('/v1/skills/runtimes')
  }, [])

  const deleteSkill = useCallback(async (id: string) => {
    try {
      await getApiClient().delete(`/v1/skills/${encodeURIComponent(id)}`)
      setSkills((prev) => prev.filter((s) => s.id !== id))
      toast.success('Skill deleted')
    } catch (err) {
      toast.error('Failed to delete skill', (err as Error).message)
      throw err
    }
  }, [])

  const atLimit = skills.length >= MAX_SKILLS_LITE

  return { skills, loading, atLimit, fetchSkills, toggleSkill, uploadSkill, checkRuntimes, deleteSkill }
}

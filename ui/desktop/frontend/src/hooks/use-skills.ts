import { useState, useEffect, useCallback } from 'react'
import { getApiClient, isApiClientReady } from '../lib/api'
import type { SkillInfo } from '../types/skill'

export const MAX_SKILLS_LITE = 10

/** Fields the gateway lets a skill owner edit. */
export interface SkillUpdate {
  name?: string
  description?: string
  visibility?: string
  tags?: string[]
}

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
  const [error, setError] = useState(false)

  const fetchSkills = useCallback(async () => {
    if (!isApiClientReady()) { setLoading(false); return }
    try {
      const res = await getApiClient().get<{ skills: SkillInfo[] | null }>('/v1/skills')
      setSkills(res.skills ?? [])
      setError(false)
    } catch (err) {
      console.error('Failed to fetch skills:', err)
      setError(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchSkills() }, [fetchSkills])

  /** Returns false (after reverting) when the gateway refuses the change. */
  const toggleSkill = useCallback(async (id: string, enabled: boolean): Promise<boolean> => {
    setSkills((prev) => prev.map((s) => s.id === id ? { ...s, enabled } : s))
    try {
      await getApiClient().post(`/v1/skills/${encodeURIComponent(id)}/toggle`, { enabled })
      return true
    } catch (err) {
      console.error('Failed to toggle skill:', err)
      setSkills((prev) => prev.map((s) => s.id === id ? { ...s, enabled: !enabled } : s))
      return false
    }
  }, [])

  const uploadSkill = useCallback(async (file: File): Promise<UploadResult> => {
    const res = await getApiClient().uploadFile<UploadResult>('/v1/skills/upload', file)
    await fetchSkills()
    return res
  }, [fetchSkills])

  const checkRuntimes = useCallback(async (): Promise<RuntimeStatus | null> => {
    if (!isApiClientReady()) return null
    return getApiClient().get<RuntimeStatus>('/v1/skills/runtimes')
  }, [])

  const deleteSkill = useCallback(async (id: string) => {
    await getApiClient().delete(`/v1/skills/${encodeURIComponent(id)}`)
    setSkills((prev) => prev.filter((s) => s.id !== id))
  }, [])

  const updateSkill = useCallback(async (id: string, updates: SkillUpdate) => {
    await getApiClient().put(`/v1/skills/${encodeURIComponent(id)}`, updates)
    setSkills((prev) => prev.map((s) => s.id === id ? { ...s, ...updates } : s))
  }, [])

  const atLimit = skills.length >= MAX_SKILLS_LITE

  return { skills, loading, error, atLimit, updateSkill, fetchSkills, toggleSkill, uploadSkill, checkRuntimes, deleteSkill }
}

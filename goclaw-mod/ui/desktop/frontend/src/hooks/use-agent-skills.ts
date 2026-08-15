import { useState, useEffect, useCallback } from 'react'
import { getApiClient, isApiClientReady } from '../lib/api'

export interface AgentSkill {
  slug: string
  name: string
  granted: boolean
}

export function useAgentSkills(agentId: string) {
  const [skills, setSkills] = useState<AgentSkill[]>([])
  const [loading, setLoading] = useState(true)

  const fetchSkills = useCallback(async () => {
    if (!isApiClientReady()) { setLoading(false); return }
    try {
      const res = await getApiClient().get<{ skills: AgentSkill[] | null }>(
        `/v1/agents/${agentId}/skills`,
      )
      setSkills(res.skills ?? [])
    } catch (err) {
      console.error('Failed to fetch agent skills:', err)
    } finally {
      setLoading(false)
    }
  }, [agentId])

  useEffect(() => { fetchSkills() }, [fetchSkills])

  return { skills, loading, refetch: fetchSkills }
}

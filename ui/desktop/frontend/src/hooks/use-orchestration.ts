import { useState, useEffect, useCallback } from 'react'
import { getApiClient, isApiClientReady } from '../lib/api'

export interface DelegateTarget {
  agent_key: string
  display_name: string
}

export interface OrchestrationInfo {
  mode: string
  delegate_targets: DelegateTarget[] | null
  team: string | null
}

export function useOrchestration(agentId: string) {
  const [mode, setMode] = useState('spawn')
  const [delegateTargets, setDelegateTargets] = useState<DelegateTarget[]>([])
  const [team, setTeam] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const fetchOrchestration = useCallback(async () => {
    if (!isApiClientReady()) { setLoading(false); return }
    try {
      const res = await getApiClient().get<OrchestrationInfo>(`/v1/agents/${agentId}/orchestration`)
      setMode(res.mode ?? 'spawn')
      setDelegateTargets(res.delegate_targets ?? [])
      setTeam(res.team ?? null)
    } catch (err) {
      console.error('Failed to fetch orchestration:', err)
    } finally {
      setLoading(false)
    }
  }, [agentId])

  useEffect(() => { fetchOrchestration() }, [fetchOrchestration])

  return { mode, delegateTargets, team, loading }
}

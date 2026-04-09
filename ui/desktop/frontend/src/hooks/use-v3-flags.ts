import { useState, useEffect, useCallback } from 'react'
import { getApiClient, isApiClientReady } from '../lib/api'
import { toast } from '../stores/toast-store'

export interface V3Flags {
  v3_pipeline_enabled: boolean
  v3_memory_enabled: boolean
  v3_retrieval_enabled: boolean
  self_evolution_metrics: boolean
  self_evolution_suggestions: boolean
}

const DEFAULT_FLAGS: V3Flags = {
  v3_pipeline_enabled: false,
  v3_memory_enabled: false,
  v3_retrieval_enabled: false,
  self_evolution_metrics: false,
  self_evolution_suggestions: false,
}

export function useV3Flags(agentId: string) {
  const [flags, setFlags] = useState<V3Flags>(DEFAULT_FLAGS)
  const [loading, setLoading] = useState(true)

  const fetchFlags = useCallback(async () => {
    if (!isApiClientReady()) { setLoading(false); return }
    try {
      const res = await getApiClient().get<V3Flags>(`/v1/agents/${agentId}/v3-flags`)
      setFlags(res)
    } catch (err) {
      console.error('Failed to fetch v3 flags:', err)
    } finally {
      setLoading(false)
    }
  }, [agentId])

  useEffect(() => { fetchFlags() }, [fetchFlags])

  const toggleFlag = useCallback(async (key: keyof V3Flags, value: boolean) => {
    setFlags((prev) => ({ ...prev, [key]: value }))
    try {
      await getApiClient().patch(`/v1/agents/${agentId}/v3-flags`, { [key]: value })
    } catch (err) {
      console.error('Failed to toggle v3 flag:', err)
      setFlags((prev) => ({ ...prev, [key]: !value }))
      toast.error('Failed to update flag', (err as Error).message)
    }
  }, [agentId])

  return { flags, loading, toggleFlag }
}

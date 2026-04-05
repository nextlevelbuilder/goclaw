import { useState, useEffect, useMemo } from 'react'
import { getApiClient } from '../lib/api'
import type { ProviderData } from '../types/provider'
import type { AgentData } from '../types/agent'

export type SetupStep = 1 | 2 | 3 | 'complete'

// Desktop is single-tenant — any provider counts as configured
function isConfigured(p: ProviderData): boolean {
  return p.enabled
}

export function useBootstrapStatus() {
  const [providers, setProviders] = useState<ProviderData[]>([])
  const [agents, setAgents] = useState<AgentData[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const doFetch = async () => {
      const api = getApiClient()
      // Fetch independently — one failing shouldn't block the other
      const [pRes, aRes] = await Promise.allSettled([
        api.get<{ providers?: ProviderData[] | null }>('/v1/providers'),
        api.get<{ agents?: AgentData[] | null }>('/v1/agents'),
      ])
      if (pRes.status === 'fulfilled') setProviders(pRes.value.providers ?? [])
      if (aRes.status === 'fulfilled') setAgents(aRes.value.agents ?? [])
      setLoading(false)
    }
    doFetch()
  }, [])

  const currentStep = useMemo<SetupStep>(() => {
    if (loading) return 1
    const hasProvider = providers.some(isConfigured)
    const hasAgent = agents.length > 0
    if (!hasProvider) return 1
    if (!hasAgent) return 2
    return 'complete'
  }, [loading, providers, agents])

  return { currentStep, loading, providers, agents }
}

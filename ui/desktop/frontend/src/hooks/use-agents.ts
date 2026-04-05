import { useEffect, useCallback } from 'react'
import { getApiClient } from '../lib/api'
import { useAgentStore } from '../stores/agent-store'
import type { Agent } from '../stores/agent-store'

// Module-level flag: prevents re-fetching agents on every component mount.
// Multiple components call useAgents() — only the first triggers the fetch.
let didFetchAgents = false

export function useAgents() {
  const { agents, selectedAgentId, setAgents, selectAgent } = useAgentStore()
  const api = getApiClient()

  const fetchAgents = useCallback(async () => {
    if (!api) return
    try {
      const result = await api.get<{ agents: Array<{
        id: string
        agent_key: string
        display_name?: string
        model?: string
        provider?: string
        other_config?: Record<string, unknown> | null
      }> }>('/v1/agents')

      const mapped: Agent[] = (result.agents ?? []).map((a) => {
        const otherCfg = a.other_config ?? {}
        return {
          id: a.id,
          key: a.agent_key,
          name: a.display_name || a.agent_key,
          model: a.model ?? 'unknown',
          status: 'online' as const,
          emoji: typeof otherCfg.emoji === 'string' ? otherCfg.emoji : undefined,
        }
      })

      setAgents(mapped)
      return mapped
    } catch (err) {
      console.error('Failed to fetch agents:', err)
      return []
    }
  }, [api, setAgents])

  // Fetch once globally, auto-select first agent only if none selected
  useEffect(() => {
    if (didFetchAgents) return
    didFetchAgents = true
    fetchAgents().then((mapped) => {
      // Only auto-select if no agent is currently selected (survives remounts)
      if (!useAgentStore.getState().selectedAgentId && mapped && mapped.length > 0) {
        selectAgent(mapped[0].id)
      }
    })
  }, [fetchAgents, selectAgent])

  const selectedAgent = agents.find((a) => a.id === selectedAgentId) ?? null

  return {
    agents,
    selectedAgent,
    selectedAgentId,
    selectAgent,
    refreshAgents: () => {
      didFetchAgents = false // allow re-fetch
      return fetchAgents()
    },
  }
}

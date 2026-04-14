import { useEffect, useCallback } from 'react'
import { agentService } from '../services/agent-service'
import { useAgentStore } from '../stores/agent-store'
import type { Agent } from '../stores/agent-store'

// Module-level flag: prevents re-fetching agents on every component mount.
// Multiple components call useAgents() — only the first triggers the fetch.
let didFetchAgents = false

export function useAgents() {
  const { agents, selectedAgentId, setAgents, selectAgent } = useAgentStore()

  const fetchAgents = useCallback(async () => {
    try {
      const result = await agentService.listItems()

      const mapped: Agent[] = (result.agents ?? []).map((a) => ({
        id: a.id,
        key: a.agent_key,
        name: a.display_name || a.agent_key,
        model: a.model ?? 'unknown',
        status: 'online' as const,
        emoji: a.emoji ?? (typeof a.other_config?.emoji === 'string' ? a.other_config.emoji : undefined),
      }))

      setAgents(mapped)
      return mapped
    } catch (err) {
      console.error('Failed to fetch agents:', err)
      return []
    }
  }, [setAgents])

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

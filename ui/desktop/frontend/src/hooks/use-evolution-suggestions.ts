import { useState, useEffect, useCallback } from 'react'
import { getApiClient, isApiClientReady } from '../lib/api'
import { toast } from '../stores/toast-store'
import type { EvolutionSuggestion } from '../types/evolution'

export function useEvolutionSuggestions(agentId: string) {
  const [suggestions, setSuggestions] = useState<EvolutionSuggestion[]>([])
  const [loading, setLoading] = useState(true)

  const fetchSuggestions = useCallback(async () => {
    if (!isApiClientReady()) { setLoading(false); return }
    try {
      const res = await getApiClient().getWithParams<{ suggestions: EvolutionSuggestion[] | null }>(
        `/v1/agents/${agentId}/evolution/suggestions`,
        { limit: '100' },
      )
      setSuggestions(res.suggestions ?? [])
    } catch (err) {
      console.error('Failed to fetch evolution suggestions:', err)
    } finally {
      setLoading(false)
    }
  }, [agentId])

  useEffect(() => { fetchSuggestions() }, [fetchSuggestions])

  const updateStatus = useCallback(async (suggestionId: string, status: 'approved' | 'rejected' | 'rolled_back') => {
    try {
      await getApiClient().patch(`/v1/agents/${agentId}/evolution/suggestions/${suggestionId}`, { status })
      setSuggestions((prev) => prev.map((s) => s.id === suggestionId ? { ...s, status } : s))
    } catch (err) {
      console.error('Failed to update suggestion:', err)
      toast.error('Failed to update suggestion', (err as Error).message)
    }
  }, [agentId])

  return { suggestions, loading, updateStatus }
}

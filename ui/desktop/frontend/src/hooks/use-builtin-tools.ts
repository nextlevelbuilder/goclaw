import { useState, useEffect, useCallback } from 'react'
import { getApiClient } from '../lib/api'
import type { BuiltinToolData } from '../types/builtin-tool'

export function useBuiltinTools() {
  const [tools, setTools] = useState<BuiltinToolData[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)

  const fetchTools = useCallback(async () => {
    try {
      const res = await getApiClient().get<{ tools: BuiltinToolData[] | null }>('/v1/tools/builtin')
      setTools(res.tools ?? [])
      setError(false)
    } catch (err) {
      console.error('Failed to fetch builtin tools:', err)
      setError(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchTools() }, [fetchTools])

  /** Returns false (after reverting) when the gateway refuses the change. */
  const toggleTool = useCallback(async (name: string, enabled: boolean): Promise<boolean> => {
    // Optimistic update
    setTools((prev) => prev.map((t) => t.name === name ? { ...t, enabled } : t))
    try {
      await getApiClient().put(`/v1/tools/builtin/${encodeURIComponent(name)}`, { enabled })
      return true
    } catch (err) {
      console.error('Failed to toggle tool:', err)
      // Revert on error
      setTools((prev) => prev.map((t) => t.name === name ? { ...t, enabled: !enabled } : t))
      return false
    }
  }, [])

  const updateSettings = useCallback(async (name: string, settings: Record<string, unknown>) => {
    await getApiClient().put(`/v1/tools/builtin/${encodeURIComponent(name)}`, { settings })
    await fetchTools()
  }, [fetchTools])

  return { tools, loading, error, fetchTools, toggleTool, updateSettings }
}

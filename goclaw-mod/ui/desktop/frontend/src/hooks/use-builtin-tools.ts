import { useState, useEffect, useCallback } from 'react'
import { getApiClient } from '../lib/api'
import { toast } from '../stores/toast-store'
import type { BuiltinToolData } from '../types/builtin-tool'

export function useBuiltinTools() {
  const [tools, setTools] = useState<BuiltinToolData[]>([])
  const [loading, setLoading] = useState(true)

  const fetchTools = useCallback(async () => {
    try {
      const res = await getApiClient().get<{ tools: BuiltinToolData[] | null }>('/v1/tools/builtin')
      setTools(res.tools ?? [])
    } catch (err) {
      console.error('Failed to fetch builtin tools:', err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchTools() }, [fetchTools])

  const toggleTool = useCallback(async (name: string, enabled: boolean) => {
    // Optimistic update
    setTools((prev) => prev.map((t) => t.name === name ? { ...t, enabled } : t))
    try {
      await getApiClient().put(`/v1/tools/builtin/${encodeURIComponent(name)}`, { enabled })
    } catch (err) {
      console.error('Failed to toggle tool:', err)
      // Revert on error
      setTools((prev) => prev.map((t) => t.name === name ? { ...t, enabled: !enabled } : t))
      toast.error('Failed to toggle tool', (err as Error).message)
    }
  }, [])

  const updateSettings = useCallback(async (name: string, settings: Record<string, unknown>) => {
    await getApiClient().put(`/v1/tools/builtin/${encodeURIComponent(name)}`, { settings })
    await fetchTools()
  }, [fetchTools])

  return { tools, loading, fetchTools, toggleTool, updateSettings }
}

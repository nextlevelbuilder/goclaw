import { useState, useEffect, useCallback } from 'react'
import { agentService } from '../services/agent-service'
import type { AgentData, AgentInput } from '../types/agent'

export const MAX_AGENTS_LITE = 5

/** Result of a bulk action: ids that succeeded and ids that failed. */
export interface BulkResult {
  done: string[]
  failed: string[]
}

/**
 * Agent list state and writes. Writes throw on failure and never show toasts:
 * the screen reports success and failure in the user's language.
 */
export function useAgentCrud() {
  const [agents, setAgents] = useState<AgentData[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(false)

  const fetchAgents = useCallback(async () => {
    try {
      const res = await agentService.list()
      setAgents(res.agents ?? [])
      setError(false)
    } catch (err) {
      console.error('Failed to fetch agents:', err)
      setError(true)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchAgents() }, [fetchAgents])

  const createAgent = useCallback(async (input: AgentInput) => {
    const res = await agentService.create(input)
    setAgents((prev) => [...prev, res])
    return res
  }, [])

  const updateAgent = useCallback(async (id: string, input: Partial<AgentData>) => {
    const res = await agentService.update(id, input)
    setAgents((prev) => prev.map((a) => a.id === id ? res : a))
    return res
  }, [])

  const deleteAgent = useCallback(async (id: string) => {
    await agentService.delete(id)
    setAgents((prev) => prev.filter((a) => a.id !== id))
  }, [])

  /** Enable (status active) or disable (status idle) several agents; one failure does not stop the rest. */
  const setAgentsEnabled = useCallback(async (ids: string[], enabled: boolean): Promise<BulkResult> => {
    const status = enabled ? 'active' : 'idle'
    const results = await Promise.allSettled(ids.map((id) => agentService.update(id, { status })))
    const done: string[] = []
    const failed: string[] = []
    const updated = new Map<string, AgentData>()
    results.forEach((r, i) => {
      if (r.status === 'fulfilled') {
        done.push(ids[i])
        updated.set(ids[i], r.value)
      } else {
        failed.push(ids[i])
      }
    })
    if (updated.size > 0) setAgents((prev) => prev.map((a) => updated.get(a.id) ?? a))
    return { done, failed }
  }, [])

  const deleteAgents = useCallback(async (ids: string[]): Promise<BulkResult> => {
    const results = await Promise.allSettled(ids.map((id) => agentService.delete(id)))
    const done = ids.filter((_, i) => results[i].status === 'fulfilled')
    const failed = ids.filter((_, i) => results[i].status === 'rejected')
    if (done.length > 0) setAgents((prev) => prev.filter((a) => !done.includes(a.id)))
    return { done, failed }
  }, [])

  const resummonAgent = useCallback(async (id: string) => {
    await agentService.resummon(id)
    setAgents((prev) => prev.map((a) => a.id === id ? { ...a, status: 'summoning' } : a))
  }, [])

  const cancelSummonAgent = useCallback(async (id: string) => {
    await agentService.cancelSummon(id)
  }, [])

  const atLimit = agents.length >= MAX_AGENTS_LITE

  return {
    agents, loading, error, atLimit, maxAgents: MAX_AGENTS_LITE, fetchAgents,
    createAgent, updateAgent, deleteAgent, setAgentsEnabled, deleteAgents, resummonAgent, cancelSummonAgent,
  }
}

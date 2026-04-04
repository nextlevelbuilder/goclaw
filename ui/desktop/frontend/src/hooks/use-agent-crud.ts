import { useState, useEffect, useCallback } from 'react'
import { agentService } from '../services/agent-service'
import { toast } from '../stores/toast-store'
import type { AgentData, AgentInput } from '../types/agent'

const MAX_AGENTS_LITE = 5

export function useAgentCrud() {
  const [agents, setAgents] = useState<AgentData[]>([])
  const [loading, setLoading] = useState(true)

  const fetchAgents = useCallback(async () => {
    try {
      const res = await agentService.list()
      setAgents(res.agents ?? [])
    } catch (err) {
      console.error('Failed to fetch agents:', err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchAgents() }, [fetchAgents])

  const createAgent = useCallback(async (input: AgentInput) => {
    try {
      const res = await agentService.create(input)
      setAgents((prev) => [...prev, res])
      toast.success('Agent created')
      return res
    } catch (err) {
      toast.error('Failed to create agent', (err as Error).message)
      throw err
    }
  }, [])

  const updateAgent = useCallback(async (id: string, input: Partial<AgentData>) => {
    try {
      const res = await agentService.update(id, input)
      setAgents((prev) => prev.map((a) => a.id === id ? res : a))
      toast.success('Agent updated')
      return res
    } catch (err) {
      toast.error('Failed to update agent', (err as Error).message)
      throw err
    }
  }, [])

  const deleteAgent = useCallback(async (id: string) => {
    try {
      await agentService.delete(id)
      setAgents((prev) => prev.filter((a) => a.id !== id))
      toast.success('Agent deleted')
    } catch (err) {
      toast.error('Failed to delete agent', (err as Error).message)
      throw err
    }
  }, [])

  const resummonAgent = useCallback(async (id: string) => {
    await agentService.resummon(id)
    // Update status to summoning
    setAgents((prev) => prev.map((a) => a.id === id ? { ...a, status: 'summoning' } : a))
  }, [])

  const atLimit = agents.length >= MAX_AGENTS_LITE

  return { agents, loading, atLimit, fetchAgents, createAgent, updateAgent, deleteAgent, resummonAgent }
}

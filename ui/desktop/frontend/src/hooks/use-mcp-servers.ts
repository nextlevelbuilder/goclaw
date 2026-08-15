import { useState, useEffect, useCallback } from 'react'
import { mcpService } from '../services/mcp-service'
import { toast } from '../stores/toast-store'
import type { MCPServerData, MCPServerInput, MCPAgentGrant, MCPToolInfo } from '../types/mcp'

export const MAX_MCP_LITE = 5

export function useMcpServers() {
  const [servers, setServers] = useState<MCPServerData[]>([])
  const [loading, setLoading] = useState(true)

  const fetchServers = useCallback(async () => {
    try {
      const res = await mcpService.list()
      setServers(res.servers ?? [])
    } catch (err) {
      console.error('Failed to fetch MCP servers:', err)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { fetchServers() }, [fetchServers])

  const createServer = useCallback(async (input: MCPServerInput) => {
    try {
      const res = await mcpService.create(input)
      await fetchServers()
      toast.success('Server created')
      return res
    } catch (err) {
      toast.error('Failed to create server', (err as Error).message)
      throw err
    }
  }, [fetchServers])

  const updateServer = useCallback(async (id: string, input: Partial<MCPServerInput>) => {
    try {
      const res = await mcpService.update(id, input)
      await fetchServers()
      toast.success('Server updated')
      return res
    } catch (err) {
      toast.error('Failed to update server', (err as Error).message)
      throw err
    }
  }, [fetchServers])

  const deleteServer = useCallback(async (id: string) => {
    try {
      await mcpService.delete(id)
      setServers((prev) => prev.filter((s) => s.id !== id))
      toast.success('Server deleted')
    } catch (err) {
      toast.error('Failed to delete server', (err as Error).message)
      throw err
    }
  }, [])

  const testConnection = useCallback(async (data: {
    transport: string
    command?: string
    args?: string[]
    url?: string
    headers?: Record<string, string>
    env?: Record<string, string>
  }) => {
    return mcpService.testConnection(data)
  }, [])

  const reconnectServer = useCallback(async (id: string) => {
    try {
      await mcpService.reconnect(id)
      toast.success('Connection reset')
    } catch (err) {
      toast.error('Failed to reconnect', (err as Error).message)
      throw err
    }
  }, [])

  const listServerTools = useCallback(async (serverId: string): Promise<MCPToolInfo[]> => {
    const res = await mcpService.listTools(serverId)
    return res.tools ?? []
  }, [])

  const listGrants = useCallback(async (serverId: string): Promise<MCPAgentGrant[]> => {
    const res = await mcpService.listGrants(serverId)
    return res.grants ?? []
  }, [])

  const grantAgent = useCallback(async (serverId: string, agentId: string) => {
    await mcpService.grantAgent(serverId, agentId)
    await fetchServers()
  }, [fetchServers])

  const revokeAgent = useCallback(async (serverId: string, agentId: string) => {
    await mcpService.revokeAgent(serverId, agentId)
    await fetchServers()
  }, [fetchServers])

  const atLimit = servers.length >= MAX_MCP_LITE

  return {
    servers, loading, atLimit,
    fetchServers, createServer, updateServer, deleteServer,
    testConnection, reconnectServer, listServerTools,
    listGrants, grantAgent, revokeAgent,
  }
}

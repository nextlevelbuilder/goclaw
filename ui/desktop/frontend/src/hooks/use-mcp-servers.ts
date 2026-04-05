import { useState, useEffect, useCallback } from 'react'
import { getApiClient } from '../lib/api'
import { toast } from '../stores/toast-store'
import type { MCPServerData, MCPServerInput, MCPAgentGrant, MCPToolInfo, MCPTestResult } from '../types/mcp'

export const MAX_MCP_LITE = 5

export function useMcpServers() {
  const [servers, setServers] = useState<MCPServerData[]>([])
  const [loading, setLoading] = useState(true)

  const fetchServers = useCallback(async () => {
    try {
      const res = await getApiClient().get<{ servers: MCPServerData[] | null }>('/v1/mcp/servers')
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
      const res = await getApiClient().post<MCPServerData>('/v1/mcp/servers', input)
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
      const res = await getApiClient().put<MCPServerData>(`/v1/mcp/servers/${id}`, input)
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
      await getApiClient().delete(`/v1/mcp/servers/${id}`)
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
    return getApiClient().post<MCPTestResult>('/v1/mcp/servers/test', data)
  }, [])

  const reconnectServer = useCallback(async (id: string) => {
    try {
      await getApiClient().post(`/v1/mcp/servers/${id}/reconnect`, {})
      toast.success('Connection reset')
    } catch (err) {
      toast.error('Failed to reconnect', (err as Error).message)
      throw err
    }
  }, [])

  const listServerTools = useCallback(async (serverId: string) => {
    const res = await getApiClient().get<{ tools: MCPToolInfo[] | null }>(`/v1/mcp/servers/${serverId}/tools`)
    return res.tools ?? []
  }, [])

  const listGrants = useCallback(async (serverId: string) => {
    const res = await getApiClient().get<{ grants: MCPAgentGrant[] | null }>(`/v1/mcp/servers/${serverId}/grants`)
    return res.grants ?? []
  }, [])

  const grantAgent = useCallback(async (serverId: string, agentId: string) => {
    await getApiClient().post(`/v1/mcp/servers/${serverId}/grants/agent`, { agent_id: agentId })
    await fetchServers()
  }, [fetchServers])

  const revokeAgent = useCallback(async (serverId: string, agentId: string) => {
    await getApiClient().delete(`/v1/mcp/servers/${serverId}/grants/agent/${agentId}`)
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

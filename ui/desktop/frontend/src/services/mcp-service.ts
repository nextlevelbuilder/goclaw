// MCP service — wraps HTTP /v1/mcp/* calls
import { getApiClient } from '../lib/api'
import type { MCPServerData, MCPServerInput, MCPAgentGrant, MCPToolInfo, MCPTestResult } from '../types/mcp'

export interface McpTestConnectionInput {
  transport: string
  command?: string
  args?: string[]
  url?: string
  headers?: Record<string, string>
  env?: Record<string, string>
}

export const mcpService = {
  list(): Promise<{ servers: MCPServerData[] | null }> {
    return getApiClient().get<{ servers: MCPServerData[] | null }>('/v1/mcp/servers')
  },

  create(input: MCPServerInput): Promise<MCPServerData> {
    return getApiClient().post<MCPServerData>('/v1/mcp/servers', input)
  },

  update(id: string, input: Partial<MCPServerInput>): Promise<MCPServerData> {
    return getApiClient().put<MCPServerData>(`/v1/mcp/servers/${id}`, input)
  },

  delete(id: string): Promise<void> {
    return getApiClient().delete<void>(`/v1/mcp/servers/${id}`)
  },

  testConnection(data: McpTestConnectionInput): Promise<MCPTestResult> {
    return getApiClient().post<MCPTestResult>('/v1/mcp/servers/test', data)
  },

  reconnect(id: string): Promise<void> {
    return getApiClient().post<void>(`/v1/mcp/servers/${id}/reconnect`, {})
  },

  listTools(serverId: string): Promise<{ tools: MCPToolInfo[] | null }> {
    return getApiClient().get<{ tools: MCPToolInfo[] | null }>(`/v1/mcp/servers/${serverId}/tools`)
  },

  listGrants(serverId: string): Promise<{ grants: MCPAgentGrant[] | null }> {
    return getApiClient().get<{ grants: MCPAgentGrant[] | null }>(`/v1/mcp/servers/${serverId}/grants`)
  },

  grantAgent(serverId: string, agentId: string): Promise<void> {
    return getApiClient().post<void>(`/v1/mcp/servers/${serverId}/grants/agent`, { agent_id: agentId })
  },

  revokeAgent(serverId: string, agentId: string): Promise<void> {
    return getApiClient().delete<void>(`/v1/mcp/servers/${serverId}/grants/agent/${agentId}`)
  },
}

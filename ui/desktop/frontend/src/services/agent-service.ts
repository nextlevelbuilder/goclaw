// Agent service — wraps HTTP /v1/agents and WS agents.* calls
import { getApiClient } from '../lib/api'
import { getWsClient } from '../lib/ws'
import type { AgentData, AgentInput, BootstrapFile } from '../types/agent'

export interface AgentListItem {
  id: string
  agent_key: string
  display_name?: string
  model?: string
  provider?: string
  other_config?: Record<string, unknown> | null
}

export const agentService = {
  list(): Promise<{ agents: AgentData[] | null }> {
    return getApiClient().get<{ agents: AgentData[] | null }>('/v1/agents')
  },

  listItems(): Promise<{ agents: AgentListItem[] }> {
    return getApiClient().get<{ agents: AgentListItem[] }>('/v1/agents')
  },

  create(input: AgentInput): Promise<AgentData> {
    return getApiClient().post<AgentData>('/v1/agents', input)
  },

  update(id: string, input: Partial<AgentData>): Promise<AgentData> {
    return getApiClient().put<AgentData>(`/v1/agents/${id}`, input)
  },

  delete(id: string): Promise<void> {
    return getApiClient().delete<void>(`/v1/agents/${id}`)
  },

  resummon(id: string): Promise<void> {
    return getApiClient().post<void>(`/v1/agents/${id}/resummon`, {})
  },

  resummonWs(agentId: string): Promise<unknown> {
    return getWsClient().call('agents.resummon', { agentId })
  },

  regenerate(id: string, prompt: string): Promise<void> {
    return getApiClient().post<void>(`/v1/agents/${id}/regenerate`, { prompt })
  },

  listFiles(agentKey: string): Promise<{ files?: BootstrapFile[] }> {
    return getWsClient().call('agents.files.list', { agentId: agentKey }) as Promise<{ files?: BootstrapFile[] }>
  },

  getFile(agentKey: string, name: string): Promise<{ file?: BootstrapFile }> {
    return getWsClient().call('agents.files.get', { agentId: agentKey, name }) as Promise<{ file?: BootstrapFile }>
  },

  setFile(agentKey: string, name: string, content: string): Promise<unknown> {
    return getWsClient().call('agents.files.set', { agentId: agentKey, name, content })
  },
}

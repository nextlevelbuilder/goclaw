// Session service — wraps WS sessions.* calls
import { getWsClient } from '../lib/ws'

export interface SessionInfoResponse {
  key: string
  messageCount: number
  created: string
  updated: string
  label?: string
  channel?: string
}

export const sessionService = {
  list(agentId: string, limit = 30): Promise<{ sessions?: SessionInfoResponse[] }> {
    return getWsClient().call('sessions.list', { agentId, limit }) as Promise<{ sessions?: SessionInfoResponse[] }>
  },

  delete(key: string): Promise<unknown> {
    return getWsClient().call('sessions.delete', { key })
  },
}

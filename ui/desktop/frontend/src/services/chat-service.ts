// Chat service — wraps WS chat.* calls and media upload
import { getWsClient } from '../lib/ws'
import { getApiClient } from '../lib/api'
import type { RawHistoryMessage } from '../hooks/chat-history-mapper'

export interface ChatSendParams {
  message: string
  agentId: string
  sessionKey: string
  stream: boolean
  media?: { path: string; filename: string }[]
}

export interface ChatSessionStatusResponse {
  isRunning?: boolean
  activity?: { phase: string; tool?: string; iteration?: number }
}

export const chatService = {
  send(params: ChatSendParams): Promise<unknown> {
    return getWsClient().call('chat.send', params as unknown as Record<string, unknown>)
  },

  history(sessionKey: string): Promise<{ messages?: RawHistoryMessage[] }> {
    return getWsClient().call('chat.history', { sessionKey }) as Promise<{ messages?: RawHistoryMessage[] }>
  },

  abort(sessionKey: string): Promise<unknown> {
    return getWsClient().call('chat.abort', { sessionKey })
  },

  sessionStatus(sessionKey: string): Promise<ChatSessionStatusResponse> {
    return getWsClient().call('chat.session_status', { sessionKey }) as Promise<ChatSessionStatusResponse>
  },

  uploadMedia(file: File): Promise<{ path: string; mime_type: string; filename: string }> {
    return getApiClient().uploadFile<{ path: string; mime_type: string; filename: string }>('/v1/media/upload', file)
  },
}

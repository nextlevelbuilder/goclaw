import { toFileUrl } from './chat-file-helpers'
import type { ChatMessage, ToolCall } from '../stores/chat-store'

/** Raw message shape from WS chat.history response */
export interface RawHistoryMessage {
  id?: string
  role: string
  content?: string
  thinking?: string
  timestamp?: number
  tool_call_id?: string
  is_error?: boolean
  tool_calls?: Array<{
    id: string
    function?: { name: string; arguments?: Record<string, unknown> }
    name?: string
    input?: Record<string, unknown>
  }>
  media_refs?: Array<{
    id?: string
    mime_type?: string
    content_type?: string
    kind?: string
    path?: string
    url?: string
  }>
}

/**
 * Transform raw WS history messages into ChatMessage[] for the store.
 * Builds tool result map, filters system messages, resolves media URLs.
 */
export function mapHistoryMessages(raw: RawHistoryMessage[]): ChatMessage[] {
  // Build tool result map for enriching assistant tool_calls
  const toolResultMap = new Map<string, { content: string; isError: boolean }>()
  for (const m of raw) {
    if (m.role === 'tool' && m.tool_call_id) {
      toolResultMap.set(m.tool_call_id, {
        content: m.content ?? '',
        isError: !!m.is_error,
      })
    }
  }

  // Filter: only user + assistant messages, exclude internal system nudges
  const filtered = raw.filter((m) =>
    (m.role === 'user' || m.role === 'assistant') &&
    !(m.role === 'user' && m.content?.startsWith('[System]'))
  )

  return filtered.map((m) => ({
    id: m.id ?? crypto.randomUUID(),
    role: m.role as 'user' | 'assistant',
    content: m.content ?? '',
    timestamp: m.timestamp ?? Date.now(),
    thinkingText: m.thinking,
    toolCalls: m.tool_calls?.map((tc): ToolCall => {
      const toolResult = toolResultMap.get(tc.id)
      return {
        toolId: tc.id,
        toolName: tc.function?.name ?? tc.name ?? 'unknown',
        arguments: tc.function?.arguments ?? tc.input ?? {},
        state: (toolResult?.isError ? 'error' : 'completed') as 'error' | 'completed',
        result: toolResult && !toolResult.isError ? toolResult.content : undefined,
        error: toolResult?.isError ? toolResult.content : undefined,
      }
    }),
    media: m.media_refs?.map((ref) => ({
      type: ref.mime_type ?? ref.content_type ?? 'image',
      url: toFileUrl(ref.path ?? ref.id ?? ref.url ?? ''),
    })),
  }))
}

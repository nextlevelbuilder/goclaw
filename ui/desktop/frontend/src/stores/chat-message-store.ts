import { create } from 'zustand'

export interface ToolCall {
  toolId: string
  toolName: string
  arguments: Record<string, unknown>
  state: 'calling' | 'completed' | 'error'
  result?: string
  error?: string
}

export interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  timestamp: number
  // Assistant-only fields
  thinkingText?: string
  toolCalls?: ToolCall[]
  media?: { type: string; url: string }[]
  usage?: { inputTokens: number; outputTokens: number }
}

interface ChatMessageState {
  messages: ChatMessage[]

  addUserMessage: (content: string) => void
  addAssistantMessage: (id: string) => void
  appendChunk: (text: string) => void
  appendThinking: (text: string) => void
  addToolCall: (toolCall: Omit<ToolCall, 'state'>) => void
  updateToolResult: (toolId: string, result: string, error?: string) => void
  finalizeMessage: (content: string, usage?: ChatMessage['usage'], media?: ChatMessage['media']) => void
  appendErrorToLastMessage: (error: string) => void
  setMessages: (messages: ChatMessage[]) => void
  clear: () => void
}

export const useChatMessageStore = create<ChatMessageState>((set) => ({
  messages: [],

  addUserMessage: (content) => {
    const msg: ChatMessage = {
      id: crypto.randomUUID(),
      role: 'user',
      content,
      timestamp: Date.now(),
    }
    set((s) => ({ messages: [...s.messages, msg] }))
  },

  addAssistantMessage: (id) => {
    const msg: ChatMessage = {
      id,
      role: 'assistant',
      content: '',
      timestamp: Date.now(),
      toolCalls: [],
    }
    set((s) => ({ messages: [...s.messages, msg] }))
  },

  appendChunk: (text) => {
    set((s) => {
      const msgs = [...s.messages]
      const last = msgs[msgs.length - 1]
      if (last?.role === 'assistant') {
        msgs[msgs.length - 1] = { ...last, content: last.content + text }
      }
      return { messages: msgs }
    })
  },

  appendThinking: (text) => {
    set((s) => {
      const msgs = [...s.messages]
      const last = msgs[msgs.length - 1]
      if (last?.role === 'assistant') {
        msgs[msgs.length - 1] = {
          ...last,
          thinkingText: (last.thinkingText ?? '') + text,
        }
      }
      return { messages: msgs }
    })
  },

  addToolCall: (tc) => {
    set((s) => {
      const msgs = [...s.messages]
      const last = msgs[msgs.length - 1]
      if (last?.role === 'assistant') {
        const toolCalls = [...(last.toolCalls ?? []), { ...tc, state: 'calling' as const }]
        msgs[msgs.length - 1] = { ...last, toolCalls }
      }
      return { messages: msgs }
    })
  },

  updateToolResult: (toolId, result, error) => {
    set((s) => {
      const msgs = [...s.messages]
      const last = msgs[msgs.length - 1]
      if (last?.role === 'assistant' && last.toolCalls) {
        const toolCalls = last.toolCalls.map((tc) =>
          tc.toolId === toolId
            ? { ...tc, state: (error ? 'error' : 'completed') as ToolCall['state'], result, error }
            : tc,
        )
        msgs[msgs.length - 1] = { ...last, toolCalls }
      }
      return { messages: msgs }
    })
  },

  // Called when a run completes — sets final content/usage/media on last assistant message.
  finalizeMessage: (content, usage, media) => {
    set((s) => {
      const msgs = [...s.messages]
      const last = msgs[msgs.length - 1]
      if (last?.role === 'assistant') {
        msgs[msgs.length - 1] = {
          ...last,
          content: content || last.content,
          usage,
          media,
        }
      }
      return { messages: msgs }
    })
  },

  // Called when a run fails — appends error text if assistant message is empty.
  appendErrorToLastMessage: (error) => {
    set((s) => {
      const msgs = [...s.messages]
      const last = msgs[msgs.length - 1]
      if (last?.role === 'assistant') {
        msgs[msgs.length - 1] = { ...last, content: last.content || `Error: ${error}` }
      }
      return { messages: msgs }
    })
  },

  setMessages: (messages) => set({ messages }),
  clear: () => set({ messages: [] }),
}))

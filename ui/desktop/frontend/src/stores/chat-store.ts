import { create } from 'zustand'

interface ToolCall {
  toolId: string
  toolName: string
  arguments: Record<string, unknown>
  state: 'calling' | 'completed' | 'error'
  result?: string
  error?: string
}

interface Message {
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

interface Activity {
  phase: string // thinking, tool_exec, compacting, streaming, retrying, leader_processing
  tool?: string
  iteration?: number
}

interface ChatState {
  messages: Message[]
  isRunning: boolean
  activity: Activity | null
  currentRunId: string | null

  addUserMessage: (content: string) => void
  startRun: (runId: string) => void
  appendChunk: (text: string) => void
  appendThinking: (text: string) => void
  addToolCall: (toolCall: Omit<ToolCall, 'state'>) => void
  updateToolResult: (toolId: string, result: string, error?: string) => void
  setActivity: (activity: Activity | null) => void
  completeRun: (content: string, usage?: Message['usage'], media?: Message['media']) => void
  failRun: (error: string) => void
  cancelRun: () => void
  restoreRunning: (activity?: Activity | null) => void
  setMessages: (messages: Message[]) => void
  clear: () => void
}

export const useChatStore = create<ChatState>((set) => ({
  messages: [],
  isRunning: false,
  activity: null,
  currentRunId: null,

  addUserMessage: (content) => {
    const msg: Message = {
      id: crypto.randomUUID(),
      role: 'user',
      content,
      timestamp: Date.now(),
    }
    set((s) => ({ messages: [...s.messages, msg] }))
  },

  startRun: (runId) => {
    const msg: Message = {
      id: runId,
      role: 'assistant',
      content: '',
      timestamp: Date.now(),
      toolCalls: [],
    }
    set((s) => ({
      messages: [...s.messages, msg],
      isRunning: true,
      currentRunId: runId,
      activity: { phase: 'thinking' },
    }))
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

  setActivity: (activity) => set({ activity }),

  completeRun: (content, usage, media) => {
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
      return { messages: msgs, isRunning: false, activity: null, currentRunId: null }
    })
  },

  failRun: (error) => {
    set((s) => {
      const msgs = [...s.messages]
      const last = msgs[msgs.length - 1]
      if (last?.role === 'assistant') {
        msgs[msgs.length - 1] = { ...last, content: last.content || `Error: ${error}` }
      }
      return { messages: msgs, isRunning: false, activity: null, currentRunId: null }
    })
  },

  // Cancellation: preserve partial streamed content, just clear running state.
  cancelRun: () => set({ isRunning: false, activity: null, currentRunId: null }),

  // Restore running state on session switch (without creating a new assistant message).
  restoreRunning: (activity) => set({ isRunning: true, activity: activity ?? null }),

  setMessages: (messages) => set({ messages }),
  clear: () => set({ messages: [], isRunning: false, activity: null, currentRunId: null }),
}))

export type { Message, ToolCall, Activity, ChatState }

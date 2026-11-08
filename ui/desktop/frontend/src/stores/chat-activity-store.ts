import { create } from 'zustand'

export interface Activity {
  phase: string // thinking, tool_exec, compacting, streaming, retrying, leader_processing
  tool?: string
  iteration?: number
}

export type ChatErrorKind = 'provider' | 'interrupted' | 'failed'

/** The last run problem the chat shows as a notice under the transcript. */
export interface ChatRunError {
  kind: ChatErrorKind
  message: string
}

interface ChatActivityState {
  isRunning: boolean
  activity: Activity | null
  currentRunId: string | null
  error: ChatRunError | null

  startRun: (runId: string) => void
  setActivity: (activity: Activity | null) => void
  completeRun: () => void
  failRun: (error?: ChatRunError) => void
  cancelRun: () => void
  restoreRunning: (activity?: Activity | null) => void
  setError: (error: ChatRunError | null) => void
  clear: () => void
}

export const useChatActivityStore = create<ChatActivityState>((set) => ({
  isRunning: false,
  activity: null,
  currentRunId: null,
  error: null,

  startRun: (runId) => set({ isRunning: true, currentRunId: runId, activity: { phase: 'thinking' }, error: null }),

  setActivity: (activity) => set({ activity }),

  completeRun: () => set({ isRunning: false, activity: null, currentRunId: null, error: null }),

  failRun: (error) => set({ isRunning: false, activity: null, currentRunId: null, error: error ?? null }),

  cancelRun: () => set({ isRunning: false, activity: null, currentRunId: null }),

  // Restore running state on session switch (without creating a new assistant message).
  restoreRunning: (activity) => set({ isRunning: true, activity: activity ?? null }),

  setError: (error) => set({ error }),

  clear: () => set({ isRunning: false, activity: null, currentRunId: null, error: null }),
}))

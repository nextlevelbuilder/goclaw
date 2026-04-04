import { create } from 'zustand'

export interface Activity {
  phase: string // thinking, tool_exec, compacting, streaming, retrying, leader_processing
  tool?: string
  iteration?: number
}

interface ChatActivityState {
  isRunning: boolean
  activity: Activity | null
  currentRunId: string | null

  startRun: (runId: string) => void
  setActivity: (activity: Activity | null) => void
  completeRun: () => void
  failRun: () => void
  cancelRun: () => void
  restoreRunning: (activity?: Activity | null) => void
  clear: () => void
}

export const useChatActivityStore = create<ChatActivityState>((set) => ({
  isRunning: false,
  activity: null,
  currentRunId: null,

  startRun: (runId) => set({ isRunning: true, currentRunId: runId, activity: { phase: 'thinking' } }),

  setActivity: (activity) => set({ activity }),

  completeRun: () => set({ isRunning: false, activity: null, currentRunId: null }),

  failRun: () => set({ isRunning: false, activity: null, currentRunId: null }),

  cancelRun: () => set({ isRunning: false, activity: null, currentRunId: null }),

  // Restore running state on session switch (without creating a new assistant message).
  restoreRunning: (activity) => set({ isRunning: true, activity: activity ?? null }),

  clear: () => set({ isRunning: false, activity: null, currentRunId: null }),
}))

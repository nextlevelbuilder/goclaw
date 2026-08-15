import { create } from 'zustand'

type AgentStatus = 'online' | 'busy' | 'offline' | 'idle'

interface Agent {
  id: string
  key: string
  name: string
  model: string
  status: AgentStatus
  emoji?: string
  busyDuration?: number // ms since busy started
}

interface AgentState {
  agents: Agent[]
  selectedAgentId: string | null

  setAgents: (agents: Agent[]) => void
  selectAgent: (id: string | null) => void
  updateAgentStatus: (id: string, status: AgentStatus) => void
}

export const useAgentStore = create<AgentState>((set) => ({
  agents: [],
  selectedAgentId: null,

  setAgents: (agents) => set({ agents }),
  selectAgent: (id) => set({ selectedAgentId: id }),
  updateAgentStatus: (id, status) =>
    set((s) => ({
      agents: s.agents.map((a) => (a.id === id ? { ...a, status } : a)),
    })),
}))

export type { Agent, AgentStatus }

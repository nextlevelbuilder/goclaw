import { create } from 'zustand'

interface Session {
  key: string
  agentId: string
  title: string
  lastMessageAt: number
  messageCount: number
}

interface SessionState {
  sessions: Session[]
  activeSessionKey: string | null

  setSessions: (sessions: Session[]) => void
  setActiveSession: (key: string | null) => void
  addSession: (session: Session) => void
  updateSession: (key: string, updates: Partial<Session>) => void
  removeSession: (key: string) => void
}

export const useSessionStore = create<SessionState>((set) => ({
  sessions: [],
  activeSessionKey: null,

  setSessions: (sessions) => set({ sessions }),
  setActiveSession: (key) => set({ activeSessionKey: key }),
  addSession: (session) => set((s) => ({ sessions: [session, ...s.sessions] })),
  updateSession: (key, updates) =>
    set((s) => ({
      sessions: s.sessions.map((sess) =>
        sess.key === key ? { ...sess, ...updates } : sess,
      ),
    })),
  removeSession: (key) =>
    set((s) => ({
      sessions: s.sessions.filter((sess) => sess.key !== key),
      activeSessionKey: s.activeSessionKey === key ? null : s.activeSessionKey,
    })),
}))

export type { Session }

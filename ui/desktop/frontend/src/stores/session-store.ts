import { create } from 'zustand'

interface Session {
  key: string
  agentId: string
  title: string
  lastMessageAt: number
  messageCount: number
}

/** Load state of the session list: idle until the first fetch is requested. */
type SessionLoadStatus = 'idle' | 'loading' | 'ready' | 'error'

interface SessionState {
  sessions: Session[]
  activeSessionKey: string | null
  status: SessionLoadStatus
  error: string | null
  /** Bumped by `reload()`; `useSessions` refetches whenever it changes. */
  reloadNonce: number

  setSessions: (sessions: Session[]) => void
  setActiveSession: (key: string | null) => void
  addSession: (session: Session) => void
  updateSession: (key: string, updates: Partial<Session>) => void
  removeSession: (key: string) => void
  setStatus: (status: SessionLoadStatus, error?: string | null) => void
  reload: () => void
}

export const useSessionStore = create<SessionState>((set) => ({
  sessions: [],
  activeSessionKey: null,
  status: 'idle',
  error: null,
  reloadNonce: 0,

  setSessions: (sessions) => set({ sessions, status: 'ready', error: null }),
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
  setStatus: (status, error = null) => set({ status, error }),
  reload: () => set((s) => ({ reloadNonce: s.reloadNonce + 1 })),
}))

export type { Session, SessionLoadStatus }

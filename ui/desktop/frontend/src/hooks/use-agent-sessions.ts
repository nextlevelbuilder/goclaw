import { useCallback, useEffect, useState } from 'react'
import { sessionService } from '../services/session-service'
import type { Session, SessionLoadStatus } from '../stores/session-store'
import { mapSessionList } from './use-sessions'

interface AgentSessions {
  sessions: Session[]
  status: SessionLoadStatus
  error: string | null
  reload: () => void
  /** Drops a conversation from this list after it was deleted. */
  remove: (key: string) => void
  /** Updates a conversation in this list after it was renamed. */
  patch: (key: string, updates: Partial<Session>) => void
}

/**
 * One agent's conversations for a view that filters on its own. Unlike the shell's
 * `useSessions()`, picking an agent here does not change the app-wide selection, so
 * the open chat is left alone.
 */
export function useAgentSessions(agentId: string | null): AgentSessions {
  const [sessions, setSessions] = useState<Session[]>([])
  const [status, setStatus] = useState<SessionLoadStatus>('idle')
  const [error, setError] = useState<string | null>(null)
  const [nonce, setNonce] = useState(0)

  useEffect(() => {
    if (!agentId) { setSessions([]); setError(null); setStatus('ready'); return }
    let cancelled = false
    setStatus('loading')
    setError(null)
    sessionService.list(agentId)
      .then((result) => {
        if (cancelled) return
        setSessions(mapSessionList(agentId, result))
        setStatus('ready')
      })
      .catch((err: unknown) => {
        if (cancelled) return
        console.error('Failed to load sessions:', err)
        setError(err instanceof Error ? err.message : String(err))
        setStatus('error')
      })
    return () => { cancelled = true }
  }, [agentId, nonce])

  const reload = useCallback(() => setNonce((n) => n + 1), [])
  const remove = useCallback((key: string) => setSessions((prev) => prev.filter((s) => s.key !== key)), [])
  const patch = useCallback(
    (key: string, updates: Partial<Session>) => setSessions((prev) => prev.map((s) => (s.key === key ? { ...s, ...updates } : s))),
    [],
  )

  return { sessions, status, error, reload, remove, patch }
}

import { useEffect, useCallback } from 'react'
import { getWsClient } from '../lib/ws'
import { useSessionStore } from '../stores/session-store'
import { useAgentStore } from '../stores/agent-store'
import { useChatStore } from '../stores/chat-store'

// Backend SessionInfo: { key, messageCount, created, updated, label, channel, userID }
interface SessionInfoResponse {
  key: string
  messageCount: number
  created: string  // ISO timestamp
  updated: string  // ISO timestamp
  label?: string
  channel?: string
}

export function useSessions() {
  const ws = getWsClient()
  const { sessions, activeSessionKey, setActiveSession, setSessions, removeSession } = useSessionStore()
  const selectedAgentId = useAgentStore((s) => s.selectedAgentId)

  // When agent changes, clear active session + chat, then fetch new sessions
  useEffect(() => {
    if (!ws || !selectedAgentId) return
    setActiveSession(null)
    useChatStore.getState().clear()
    let cancelled = false
    ws.call('sessions.list', { agentId: selectedAgentId, limit: 30 })
      .then((result: unknown) => {
        if (cancelled) return
        const r = result as { sessions?: SessionInfoResponse[] }
        const list = (r?.sessions || []).map((s) => ({
          key: s.key,
          agentId: selectedAgentId,
          title: s.label || 'Untitled',
          lastMessageAt: new Date(s.updated || s.created).getTime(),
          messageCount: s.messageCount || 0,
        }))
        setSessions(list)
      })
      .catch(console.error)
    return () => { cancelled = true }
  }, [ws, selectedAgentId, setSessions, setActiveSession])

  // "New Chat" just clears active session + chat.
  // Actual session is created by sendMessage on first message (auto-session-creation).
  const createSession = useCallback(() => {
    setActiveSession(null)
    useChatStore.getState().clear()
  }, [setActiveSession])

  const deleteSession = useCallback(async (key: string) => {
    try {
      await ws.call('sessions.delete', { key })
    } catch { /* best effort */ }
    removeSession(key)
    if (activeSessionKey === key) {
      setActiveSession(null)
      useChatStore.getState().clear()
    }
  }, [ws, activeSessionKey, removeSession, setActiveSession])

  return { sessions, activeSessionKey, setActiveSession, createSession, deleteSession }
}

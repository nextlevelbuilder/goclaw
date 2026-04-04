import { useEffect, useCallback } from 'react'
import { sessionService } from '../services/session-service'
import { useSessionStore } from '../stores/session-store'
import { useAgentStore } from '../stores/agent-store'
import { useChatMessageStore } from '../stores/chat-message-store'
import { useChatActivityStore } from '../stores/chat-activity-store'

export function useSessions() {
  const { sessions, activeSessionKey, setActiveSession, setSessions, removeSession } = useSessionStore()
  const selectedAgentId = useAgentStore((s) => s.selectedAgentId)

  // When agent changes, clear active session + chat, then fetch new sessions
  useEffect(() => {
    if (!selectedAgentId) return
    setActiveSession(null)
    useChatMessageStore.getState().clear()
    useChatActivityStore.getState().clear()
    let cancelled = false
    sessionService.list(selectedAgentId)
      .then((result) => {
        if (cancelled) return
        const list = (result?.sessions || []).map((s) => ({
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
  }, [selectedAgentId, setSessions, setActiveSession])

  // "New Chat" just clears active session + chat.
  // Actual session is created by sendMessage on first message (auto-session-creation).
  const createSession = useCallback(() => {
    setActiveSession(null)
    useChatMessageStore.getState().clear()
    useChatActivityStore.getState().clear()
  }, [setActiveSession])

  const deleteSession = useCallback(async (key: string) => {
    try {
      await sessionService.delete(key)
    } catch { /* best effort */ }
    removeSession(key)
    if (activeSessionKey === key) {
      setActiveSession(null)
      useChatMessageStore.getState().clear()
      useChatActivityStore.getState().clear()
    }
  }, [activeSessionKey, removeSession, setActiveSession])

  return { sessions, activeSessionKey, setActiveSession, createSession, deleteSession }
}

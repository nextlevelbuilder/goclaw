import { useEffect, useCallback } from 'react'
import i18n from 'i18next'
import { useTranslation } from 'react-i18next'
import { sessionService } from '../services/session-service'
import { useSessionStore, type Session } from '../stores/session-store'
import { useAgentStore } from '../stores/agent-store'
import { useChatMessageStore } from '../stores/chat-message-store'
import { useChatActivityStore } from '../stores/chat-activity-store'
import { toast } from '../components/ui'
import { deleteSessionByKey, renameSessionByKey } from './chat-session-actions'

/** Maps a `sessions.list` reply to store sessions for one agent. */
export function mapSessionList(agentId: string, result: Awaited<ReturnType<typeof sessionService.list>> | undefined): Session[] {
  return (result?.sessions || []).map((s) => ({
    key: s.key,
    agentId,
    title: s.label || i18n.t('sessions:untitled'),
    lastMessageAt: new Date(s.updated || s.created).getTime(),
    messageCount: s.messageCount || 0,
  }))
}

export function useSessions() {
  const { t } = useTranslation('sessions')
  const { sessions, activeSessionKey, status, error, reloadNonce, setActiveSession, setSessions, setStatus, reload } = useSessionStore()
  const selectedAgentId = useAgentStore((s) => s.selectedAgentId)

  // When the agent changes, clear the active session and the chat.
  useEffect(() => {
    if (!selectedAgentId) return
    setActiveSession(null)
    useChatMessageStore.getState().clear()
    useChatActivityStore.getState().clear()
  }, [selectedAgentId, setActiveSession])

  // Fetch the agent's sessions on agent change and whenever `reload()` is called.
  useEffect(() => {
    // No agent yet: there is nothing to list, so the list is loaded and empty (not loading forever).
    if (!selectedAgentId) { setSessions([]); return }
    let cancelled = false
    setStatus('loading')
    sessionService.list(selectedAgentId)
      .then((result) => {
        if (cancelled) return
        setSessions(mapSessionList(selectedAgentId, result))
      })
      .catch((err: unknown) => {
        if (cancelled) return
        console.error('Failed to load sessions:', err)
        setStatus('error', err instanceof Error ? err.message : String(err))
      })
    return () => { cancelled = true }
  }, [selectedAgentId, reloadNonce, setSessions, setStatus])

  // "New Chat" just clears active session + chat.
  // Actual session is created by sendMessage on first message (auto-session-creation).
  const createSession = useCallback(() => {
    setActiveSession(null)
    useChatMessageStore.getState().clear()
    useChatActivityStore.getState().clear()
  }, [setActiveSession])

  const deleteSession = useCallback(async (key: string) => {
    try {
      await deleteSessionByKey(key)
    } catch {
      toast.error(t('toast.deleteFailed'))
    }
  }, [t])

  const renameSession = useCallback(async (key: string, label: string) => {
    try {
      await renameSessionByKey(key, label)
    } catch {
      toast.error(t('toast.updateFailed'))
    }
  }, [t])

  return { sessions, activeSessionKey, status, error, setActiveSession, createSession, deleteSession, renameSession, reload }
}

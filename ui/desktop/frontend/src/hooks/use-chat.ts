import { useCallback } from 'react'
import i18n from '../i18n'
import { getWsClient } from '../lib/ws'
import { chatService } from '../services/chat-service'
import { useChatMessageStore } from '../stores/chat-message-store'
import { useChatActivityStore } from '../stores/chat-activity-store'
import { useChatModelStore } from '../stores/chat-model-store'
import { useSessionStore } from '../stores/session-store'
import { loadChatHistory } from './use-chat-events'
import type { AttachedFile } from '../components/chat/InputBar'

type UploadedMedia = { path: string; filename: string }

/** What the last request carried, so Retry can repeat it with its attachments. */
interface LastSent {
  sessionKey: string
  text: string
  media?: UploadedMedia[]
}

/** Module-level so Retry still has the attachments after the chat screen was left and reopened. */
let lastSent: LastSent | null = null

/**
 * Chat state and actions for the chat screen. Agent events are consumed once by
 * `useChatEvents()` in the app shell; this hook reads the stores they fill.
 */
export function useChat() {
  const ws = getWsClient()
  const messages = useChatMessageStore((s) => s.messages)
  const addUserMessage = useChatMessageStore((s) => s.addUserMessage)
  const isRunning = useChatActivityStore((s) => s.isRunning)
  const activity = useChatActivityStore((s) => s.activity)
  const error = useChatActivityStore((s) => s.error)
  const failRun = useChatActivityStore((s) => s.failRun)

  const activeSessionKey = useSessionStore((s) => s.activeSessionKey)

  const sendMessage = useCallback(
    async (
      text: string,
      agentId: string,
      attachedFiles?: AttachedFile[],
      opts?: { resend?: boolean; media?: UploadedMedia[] },
    ) => {
      if (!ws || (!text.trim() && !attachedFiles?.length && !opts?.media?.length)) return

      let sessionKey = activeSessionKey
      if (!sessionKey) {
        sessionKey = `agent:${agentId}:ws:direct:system:${crypto.randomUUID().slice(0, 8)}`
        useSessionStore.getState().addSession({ key: sessionKey, agentId, title: text.trim().slice(0, 40) || 'New Chat', lastMessageAt: Date.now(), messageCount: 0 })
        useSessionStore.getState().setActiveSession(sessionKey)
      }

      // A resend repeats the last user message that is already in the transcript.
      if (!opts?.resend) addUserMessage(text)
      useChatActivityStore.getState().setError(null)

      let media: UploadedMedia[] | undefined = opts?.media
      if (!media && attachedFiles?.length) {
        const uploads = await Promise.all(
          attachedFiles.map(async (af) => {
            if (af.localPath) return { path: af.localPath, filename: af.name }
            if (!af.file) return null
            try {
              const res = await chatService.uploadMedia(af.file)
              return { path: res.path, filename: res.filename }
            } catch (err) { console.error('File upload failed:', af.name, err); return null }
          }),
        )
        media = uploads.filter((u): u is UploadedMedia => u !== null)
        if (media.length === 0) media = undefined
      }
      lastSent = { sessionKey, text, media }

      // Per-request model override from the chat top bar; empty = agent model.
      const modelOverride = useChatModelStore.getState().modelOverride.trim()

      try {
        await chatService.send({
          message: text,
          agentId,
          sessionKey,
          stream: true,
          ...(modelOverride && { model: modelOverride }),
          ...(media && { media }),
        })
      } catch (err) {
        console.error('chat.send failed:', err)
        failRun({ kind: 'failed', message: err instanceof Error ? err.message : String(err) })
      }
    },
    [ws, activeSessionKey, addUserMessage, failRun],
  )

  const loadHistory = useCallback((sessionKey: string) => loadChatHistory(sessionKey), [])

  /** Sends the last request again (text and attachments) without adding it to the transcript twice. */
  const retry = useCallback(async (agentId: string) => {
    const last = lastSent
    if (last && last.sessionKey === useSessionStore.getState().activeSessionKey) {
      await sendMessage(last.text, agentId, undefined, { resend: true, media: last.media })
      return
    }
    const lastUser = [...useChatMessageStore.getState().messages].reverse().find((m) => m.role === 'user')
    if (lastUser) await sendMessage(lastUser.content, agentId, undefined, { resend: true })
  }, [sendMessage])

  /** Asks the agent to carry on after an interrupted reply. */
  const continueReply = useCallback(
    (agentId: string) => sendMessage(i18n.t('chat:notice.interrupted.continuePrompt'), agentId),
    [sendMessage],
  )

  const dismissError = useCallback(() => useChatActivityStore.getState().setError(null), [])

  const abort = useCallback(async () => {
    const sk = useSessionStore.getState().activeSessionKey
    if (!sk) return
    try { await chatService.abort(sk) } catch { /* ignore */ }
  }, [])

  return { messages, isRunning, activity, error, sendMessage, loadHistory, abort, retry, continueReply, dismissError }
}

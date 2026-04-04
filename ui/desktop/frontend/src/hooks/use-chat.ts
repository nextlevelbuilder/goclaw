import { useEffect, useRef, useCallback } from 'react'
import { getWsClient } from '../lib/ws'
import { chatService } from '../services/chat-service'
import { useChatMessageStore } from '../stores/chat-message-store'
import { useChatActivityStore } from '../stores/chat-activity-store'
import { useSessionStore } from '../stores/session-store'
import { toFileUrl } from './chat-file-helpers'
import { useStreamBatcher } from './use-stream-batcher'
import { mapHistoryMessages } from './chat-history-mapper'
import type { AttachedFile } from '../components/chat/InputBar'

export function useChat() {
  const ws = getWsClient()
  const {
    messages,
    addUserMessage, addAssistantMessage, appendChunk, appendThinking,
    addToolCall, updateToolResult, finalizeMessage, appendErrorToLastMessage, setMessages,
  } = useChatMessageStore()

  const {
    isRunning, activity,
    startRun, setActivity, completeRun, failRun,
  } = useChatActivityStore()

  const activeSessionKey = useSessionStore((s) => s.activeSessionKey)
  const sessionKeyRef = useRef(activeSessionKey)
  sessionKeyRef.current = activeSessionKey
  const currentRunIdRef = useRef<string | null>(null)

  const chunkBatcher = useStreamBatcher(
    useCallback((text: string) => appendChunk(text), [appendChunk]),
  )
  const thinkingBatcher = useStreamBatcher(
    useCallback((text: string) => appendThinking(text), [appendThinking]),
  )

  // Subscribe to agent events from WS
  useEffect(() => {
    if (!ws) return

    const unsub = ws.on('agent', (raw: unknown) => {
      const event = raw as { type: string; runId: string; sessionKey: string; payload: Record<string, unknown> }
      if (sessionKeyRef.current && event.sessionKey !== sessionKeyRef.current) return
      const p = event.payload ?? {}

      switch (event.type) {
        case 'run.started':
          currentRunIdRef.current = event.runId
          addAssistantMessage(event.runId)
          startRun(event.runId)
          break
        case 'chunk':
          chunkBatcher.append((p.content as string) ?? '')
          break
        case 'thinking':
          thinkingBatcher.append((p.content as string) ?? '')
          break
        case 'tool.call':
          chunkBatcher.flush()
          addToolCall({ toolId: (p.id as string) ?? '', toolName: (p.name as string) ?? 'unknown', arguments: (p.arguments as Record<string, unknown>) ?? {} })
          break
        case 'tool.result': {
          const isError = p.is_error as boolean
          updateToolResult((p.id as string) ?? '', isError ? '' : ((p.result as string) ?? ''), isError ? ((p.content as string) ?? (p.result as string) ?? 'Error') : undefined)
          break
        }
        case 'block.reply':
          chunkBatcher.flush()
          appendChunk((p.content as string) ?? '')
          break
        case 'activity':
          setActivity({ phase: (p.phase as string) ?? 'thinking', tool: p.tool as string | undefined, iteration: p.iteration as number | undefined })
          break
        case 'run.completed': {
          chunkBatcher.flush()
          thinkingBatcher.flush()
          const usage = p.usage as Record<string, number> | undefined
          finalizeMessage(
            (p.content as string) ?? '',
            usage ? { inputTokens: usage.prompt_tokens ?? 0, outputTokens: usage.completion_tokens ?? 0 } : undefined,
            (p.media as { path?: string; content_type?: string; url?: string; type?: string }[] | undefined)
              ?.map((m) => ({ type: m.content_type ?? m.type ?? 'file', url: toFileUrl(m.path ?? m.url ?? '') })),
          )
          completeRun()
          currentRunIdRef.current = null
          break
        }
        case 'run.failed':
          chunkBatcher.flush()
          thinkingBatcher.flush()
          appendErrorToLastMessage((p.error as string) ?? 'Unknown error')
          failRun()
          currentRunIdRef.current = null
          break
        case 'run.cancelled':
          chunkBatcher.flush()
          thinkingBatcher.flush()
          useChatActivityStore.getState().cancelRun()
          currentRunIdRef.current = null
          break
        case 'run.retrying':
          setActivity({ phase: 'retrying', tool: undefined, iteration: Number(p.attempt) || 0 })
          break
      }
    })
    return unsub
  }, [ws, addAssistantMessage, startRun, appendChunk, addToolCall, updateToolResult, setActivity, finalizeMessage, appendErrorToLastMessage, completeRun, failRun, chunkBatcher, thinkingBatcher])

  const sendMessage = useCallback(
    async (text: string, agentId: string, attachedFiles?: AttachedFile[]) => {
      if (!ws || (!text.trim() && !attachedFiles?.length)) return

      let sessionKey = activeSessionKey
      if (!sessionKey) {
        sessionKey = `agent:${agentId}:ws:direct:system:${crypto.randomUUID().slice(0, 8)}`
        const { useSessionStore } = await import('../stores/session-store')
        useSessionStore.getState().addSession({ key: sessionKey, agentId, title: text.trim().slice(0, 40) || 'New Chat', lastMessageAt: Date.now(), messageCount: 0 })
        useSessionStore.getState().setActiveSession(sessionKey)
      }

      addUserMessage(text)

      let media: { path: string; filename: string }[] | undefined
      if (attachedFiles?.length) {
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
        media = uploads.filter((u): u is { path: string; filename: string } => u !== null)
        if (media.length === 0) media = undefined
      }

      try {
        await chatService.send({ message: text, agentId, sessionKey, stream: true, ...(media && { media }) })
      } catch (err) { console.error('chat.send failed:', err) }
    },
    [ws, activeSessionKey, addUserMessage],
  )

  const loadHistory = useCallback(
    async (sessionKey: string) => {
      try {
        const result = await chatService.history(sessionKey)
        if (result?.messages) setMessages(mapHistoryMessages(result.messages))
      } catch (err) { console.error('Failed to load history:', err) }
    },
    [setMessages],
  )

  const abort = useCallback(async () => {
    const sk = sessionKeyRef.current
    if (!sk) return
    try { await chatService.abort(sk) } catch { /* ignore */ }
  }, [])

  // Reset streaming state + load history when session changes
  const prevSessionRef = useRef<string | null>(null)
  useEffect(() => {
    if (activeSessionKey === prevSessionRef.current) return
    prevSessionRef.current = activeSessionKey

    chunkBatcher.flush()
    thinkingBatcher.flush()
    currentRunIdRef.current = null

    if (!activeSessionKey) {
      useChatMessageStore.getState().clear()
      useChatActivityStore.getState().clear()
      return
    }

    let cancelled = false
    loadHistory(activeSessionKey).then(() => { if (cancelled) return })

    chatService.sessionStatus(activeSessionKey)
      .then((res) => {
        if (cancelled) return
        if (res?.isRunning) useChatActivityStore.getState().restoreRunning(res.activity ?? null)
      })
      .catch(() => {})

    return () => { cancelled = true }
  }, [activeSessionKey, loadHistory, chunkBatcher, thinkingBatcher])

  return { messages, isRunning, activity, sendMessage, loadHistory, abort }
}

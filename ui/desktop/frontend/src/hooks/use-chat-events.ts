import { useEffect, useMemo, useRef, useCallback } from 'react'
import { getWsClient } from '../lib/ws'
import { chatService } from '../services/chat-service'
import { useChatMessageStore } from '../stores/chat-message-store'
import { useChatActivityStore } from '../stores/chat-activity-store'
import { useSessionStore } from '../stores/session-store'
import { useGatewayStatus } from '../components/layout/use-gateway-status'
import { toFileUrl } from './chat-file-helpers'
import { useStreamBatcher } from './use-stream-batcher'
import { mapHistoryMessages } from './chat-history-mapper'
import { classifyRunError } from '../utils/chat-error'

/** Replaces the transcript with the saved history, unless the user moved to another conversation meanwhile. */
export async function loadChatHistory(sessionKey: string): Promise<void> {
  try {
    const result = await chatService.history(sessionKey)
    if (useSessionStore.getState().activeSessionKey !== sessionKey) return
    if (result?.messages) useChatMessageStore.getState().setMessages(mapHistoryMessages(result.messages))
  } catch (err) { console.error('Failed to load history:', err) }
}

interface AgentEvent {
  type: string
  runId: string
  sessionKey: string
  payload: Record<string, unknown>
}

const TERMINAL = new Set(['run.completed', 'run.failed', 'run.cancelled'])

/**
 * The one subscription to agent events and the owner of run state. Mounted once in
 * the app shell so a reply keeps streaming into the stores while the user is on
 * another screen; `useChat()` only reads those stores.
 */
export function useChatEvents() {
  const ws = useMemo(() => {
    try { return getWsClient() } catch { return null } // client not initialised yet (tests, startup)
  }, [])
  const { disconnected } = useGatewayStatus()
  const activeSessionKey = useSessionStore((s) => s.activeSessionKey)
  const currentRunIdRef = useRef<string | null>(null)

  const chunkBatcher = useStreamBatcher(
    useCallback((text: string) => useChatMessageStore.getState().appendChunk(text), []),
  )
  const thinkingBatcher = useStreamBatcher(
    useCallback((text: string) => useChatMessageStore.getState().appendThinking(text), []),
  )

  useEffect(() => {
    if (!ws) return

    // Events can reach a run whose start this client never saw (switched conversation,
    // reconnect): adopt the run instead of appending to whatever message is last.
    const adoptRun = (runId: string) => {
      const messages = useChatMessageStore.getState()
      if (currentRunIdRef.current !== runId) {
        currentRunIdRef.current = runId
        if (!messages.messages.some((m) => m.id === runId)) messages.addAssistantMessage(runId)
      }
      if (!useChatActivityStore.getState().isRunning) useChatActivityStore.getState().startRun(runId)
    }

    const unsub = ws.on('agent', (raw: unknown) => {
      const event = raw as AgentEvent
      const activeKey = useSessionStore.getState().activeSessionKey
      if (!activeKey || event.sessionKey !== activeKey) return
      const p = event.payload ?? {}
      const messages = useChatMessageStore.getState()
      const run = useChatActivityStore.getState()

      // A finish for a run other than the one on screen must not end the current one.
      if (TERMINAL.has(event.type) && currentRunIdRef.current && currentRunIdRef.current !== event.runId) return

      switch (event.type) {
        case 'run.started':
          currentRunIdRef.current = event.runId
          messages.addAssistantMessage(event.runId)
          run.startRun(event.runId)
          break
        case 'chunk':
          adoptRun(event.runId)
          chunkBatcher.append((p.content as string) ?? '')
          break
        case 'thinking':
          adoptRun(event.runId)
          thinkingBatcher.append((p.content as string) ?? '')
          break
        case 'tool.call':
          adoptRun(event.runId)
          chunkBatcher.flush()
          messages.addToolCall({ toolId: (p.id as string) ?? '', toolName: (p.name as string) ?? 'unknown', arguments: (p.arguments as Record<string, unknown>) ?? {} })
          break
        case 'tool.result': {
          const isError = p.is_error as boolean
          messages.updateToolResult((p.id as string) ?? '', isError ? '' : ((p.result as string) ?? ''), isError ? ((p.content as string) ?? (p.result as string) ?? 'Error') : undefined)
          break
        }
        case 'block.reply':
          adoptRun(event.runId)
          chunkBatcher.flush()
          messages.appendChunk((p.content as string) ?? '')
          break
        case 'activity':
          run.setActivity({ phase: (p.phase as string) ?? 'thinking', tool: p.tool as string | undefined, iteration: p.iteration as number | undefined })
          break
        case 'run.completed': {
          chunkBatcher.flush()
          thinkingBatcher.flush()
          if (messages.messages.some((m) => m.id === event.runId)) {
            const usage = p.usage as Record<string, number> | undefined
            messages.finalizeMessage(
              (p.content as string) ?? '',
              usage ? { inputTokens: usage.prompt_tokens ?? 0, outputTokens: usage.completion_tokens ?? 0 } : undefined,
              (p.media as { path?: string; content_type?: string; url?: string; type?: string }[] | undefined)
                ?.map((m) => ({ type: m.content_type ?? m.type ?? 'file', url: toFileUrl(m.path ?? m.url ?? '') })),
            )
          } else {
            // The reply started while this client was not watching: the saved history has it.
            void loadChatHistory(activeKey)
          }
          run.completeRun()
          currentRunIdRef.current = null
          break
        }
        case 'run.failed': {
          chunkBatcher.flush()
          thinkingBatcher.flush()
          const message = (p.error as string) ?? 'Unknown error'
          run.failRun({ kind: classifyRunError(message), message })
          currentRunIdRef.current = null
          break
        }
        case 'run.cancelled':
          chunkBatcher.flush()
          thinkingBatcher.flush()
          run.cancelRun()
          currentRunIdRef.current = null
          break
        case 'run.retrying':
          run.setActivity({ phase: 'retrying', tool: undefined, iteration: Number(p.attempt) || 0 })
          break
      }
    })
    return unsub
  }, [ws, chunkBatcher, thinkingBatcher])

  // Connection lost mid-reply ends the run with an "interrupted" notice; once back,
  // ask the gateway whether the run is still going or already saved its answer.
  const wasDisconnectedRef = useRef(false)
  useEffect(() => {
    if (disconnected) {
      wasDisconnectedRef.current = true
      chunkBatcher.flush()
      thinkingBatcher.flush()
      if (useChatActivityStore.getState().isRunning) {
        useChatActivityStore.getState().failRun({ kind: 'interrupted', message: '' })
      }
      return
    }
    if (!wasDisconnectedRef.current) return
    wasDisconnectedRef.current = false

    const key = useSessionStore.getState().activeSessionKey
    if (!key) return
    chatService.sessionStatus(key)
      .then((res) => {
        if (useSessionStore.getState().activeSessionKey !== key) return
        if (res?.isRunning) {
          useChatActivityStore.getState().setError(null)
          useChatActivityStore.getState().restoreRunning(res.activity ?? null)
        } else {
          void loadChatHistory(key)
        }
      })
      .catch(() => {})
  }, [disconnected, chunkBatcher, thinkingBatcher])

  // Switching conversation: reset streaming state, load the saved transcript and
  // reconcile the run flag with what the gateway says.
  const prevSessionRef = useRef<string | null>(null)
  useEffect(() => {
    const previous = prevSessionRef.current
    if (activeSessionKey === previous) return
    prevSessionRef.current = activeSessionKey

    chunkBatcher.flush()
    thinkingBatcher.flush()
    currentRunIdRef.current = null

    if (!activeSessionKey) {
      useChatMessageStore.getState().clear()
      useChatActivityStore.getState().clear()
      return
    }
    // A run flag left over from the previous conversation does not belong to this one.
    if (previous !== null) useChatActivityStore.getState().clear()

    let cancelled = false
    void loadChatHistory(activeSessionKey)

    chatService.sessionStatus(activeSessionKey)
      .then((res) => {
        if (cancelled) return
        if (res?.isRunning) useChatActivityStore.getState().restoreRunning(res.activity ?? null)
        else if (currentRunIdRef.current === null) useChatActivityStore.getState().completeRun()
      })
      .catch(() => {})

    return () => { cancelled = true }
  }, [activeSessionKey, chunkBatcher, thinkingBatcher])
}

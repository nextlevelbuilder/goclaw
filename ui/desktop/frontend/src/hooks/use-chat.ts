import { useEffect, useRef, useCallback } from 'react'
import { getWsClient } from '../lib/ws'
import { getApiClient, isApiClientReady } from '../lib/api'
import { useChatStore } from '../stores/chat-store'
import { useSessionStore } from '../stores/session-store'
import type { AttachedFile } from '../components/chat/InputBar'

// Convert any file path to /v1/files/ URL for serving.
// Absolute paths (from media_refs.path) use the full path so the backend
// can resolve the file directly without findInWorkspace fallback.
function toFileUrl(path: string): string {
  if (!path) return ''
  if (path.includes('/v1/files/')) return resolveBase(path)
  // Normalize backslashes (Windows paths: C:\Users\... → C:/Users/...)
  const normalized = path.replace(/\\/g, '/')
  // Absolute: Unix /... or Windows C:/...
  if (normalized.startsWith('/')) return resolveBase(`/v1/files${normalized}`)
  if (/^[a-zA-Z]:\//.test(normalized)) return resolveBase(`/v1/files/${normalized}`)
  return resolveBase(`/v1/files/${normalized.split('/').pop() ?? normalized}`)
}

function resolveBase(path: string): string {
  if (path.startsWith('http')) return path
  if (isApiClientReady()) return getApiClient().getBaseUrl() + path
  return path
}

// RAF batching — prevents 100+ setState calls/sec during streaming
function useStreamBatcher(onFlush: (text: string) => void) {
  const bufferRef = useRef('')
  const rafRef = useRef(0)

  const append = useCallback(
    (text: string) => {
      bufferRef.current += text
      if (!rafRef.current) {
        rafRef.current = requestAnimationFrame(() => {
          onFlush(bufferRef.current)
          bufferRef.current = ''
          rafRef.current = 0
        })
      }
    },
    [onFlush],
  )

  const flush = useCallback(() => {
    if (rafRef.current) {
      cancelAnimationFrame(rafRef.current)
      rafRef.current = 0
    }
    if (bufferRef.current) {
      onFlush(bufferRef.current)
      bufferRef.current = ''
    }
  }, [onFlush])

  return { append, flush }
}

export function useChat() {
  const ws = getWsClient()
  const {
    messages,
    isRunning,
    activity,
    addUserMessage,
    startRun,
    appendChunk,
    appendThinking,
    addToolCall,
    updateToolResult,
    setActivity,
    completeRun,
    failRun,
    setMessages,
  } = useChatStore()

  const activeSessionKey = useSessionStore((s) => s.activeSessionKey)
  const sessionKeyRef = useRef(activeSessionKey)
  sessionKeyRef.current = activeSessionKey // always up-to-date, no stale closure
  const currentRunIdRef = useRef<string | null>(null)

  const chunkBatcher = useStreamBatcher(
    useCallback((text: string) => appendChunk(text), [appendChunk]),
  )
  const thinkingBatcher = useStreamBatcher(
    useCallback((text: string) => appendThinking(text), [appendThinking]),
  )

  useEffect(() => {
    if (!ws) return

    const unsub = ws.on('agent', (raw: unknown) => {
      const event = raw as {
        type: string
        runId: string
        sessionKey: string
        payload: Record<string, unknown>
      }

      // Use ref to avoid stale closure — activeSessionKey may change during auto-session-creation
      if (sessionKeyRef.current && event.sessionKey !== sessionKeyRef.current) return

      const p = event.payload ?? {}

      switch (event.type) {
        case 'run.started':
          currentRunIdRef.current = event.runId
          startRun(event.runId)
          break

        // Backend sends: { content: "..." }
        case 'chunk':
          chunkBatcher.append((p.content as string) ?? '')
          break

        // Backend sends: { content: "thinking text..." }
        case 'thinking':
          thinkingBatcher.append((p.content as string) ?? '')
          break

        // Backend sends: { name, id, arguments }
        case 'tool.call':
          chunkBatcher.flush()
          addToolCall({
            toolId: (p.id as string) ?? '',
            toolName: (p.name as string) ?? 'unknown',
            arguments: (p.arguments as Record<string, unknown>) ?? {},
          })
          break

        // Backend sends: { name, id, is_error, result, content (when error), arguments }
        case 'tool.result': {
          const isError = p.is_error as boolean
          const toolId = (p.id as string) ?? ''
          updateToolResult(
            toolId,
            isError ? '' : ((p.result as string) ?? ''),
            isError ? ((p.content as string) ?? (p.result as string) ?? 'Error') : undefined,
          )
          break
        }

        // Backend sends: { content: "intermediate reply text" }
        case 'block.reply':
          // Intermediate assistant content during tool iterations — append as chunk
          chunkBatcher.flush()
          appendChunk((p.content as string) ?? '')
          break

        // Backend sends: { phase, tool, tools[], iteration }
        case 'activity':
          setActivity({
            phase: (p.phase as string) ?? 'thinking',
            tool: p.tool as string | undefined,
            iteration: p.iteration as number | undefined,
          })
          break

        // Backend sends: { content, usage: { prompt_tokens, completion_tokens, total_tokens, ... }, media }
        case 'run.completed': {
          chunkBatcher.flush()
          thinkingBatcher.flush()
          const usage = p.usage as Record<string, number> | undefined
          completeRun(
            (p.content as string) ?? '',
            usage ? {
              inputTokens: usage.prompt_tokens ?? 0,
              outputTokens: usage.completion_tokens ?? 0,
            } : undefined,
            (p.media as { path?: string; content_type?: string; url?: string; type?: string }[] | undefined)
              ?.map((m) => ({ type: m.content_type ?? m.type ?? 'file', url: toFileUrl(m.path ?? m.url ?? '') })),
          )
          currentRunIdRef.current = null
          break
        }

        // Backend sends: { error: "..." }
        case 'run.failed':
          chunkBatcher.flush()
          thinkingBatcher.flush()
          failRun((p.error as string) ?? 'Unknown error')
          currentRunIdRef.current = null
          break

        // User-initiated cancellation — preserve partial content, no error message.
        case 'run.cancelled':
          chunkBatcher.flush()
          thinkingBatcher.flush()
          useChatStore.getState().cancelRun()
          currentRunIdRef.current = null
          break

        // Backend sends: { attempt, maxAttempts, error }
        case 'run.retrying':
          setActivity({
            phase: 'retrying',
            tool: undefined,
            iteration: Number(p.attempt) || 0,
          })
          break
      }
    })

    return unsub
  }, [
    ws,
    startRun,
    appendChunk,
    addToolCall,
    updateToolResult,
    setActivity,
    completeRun,
    failRun,
    chunkBatcher,
    thinkingBatcher,
  ]) // sessionKeyRef used instead of activeSessionKey — no re-subscribe on session change

  const sendMessage = useCallback(
    async (text: string, agentId: string, attachedFiles?: AttachedFile[]) => {
      if (!ws || (!text.trim() && !attachedFiles?.length)) return

      // Auto-create session if none active
      let sessionKey = activeSessionKey
      if (!sessionKey) {
        sessionKey = `agent:${agentId}:ws:direct:system:${crypto.randomUUID().slice(0, 8)}`
        const { useSessionStore } = await import('../stores/session-store')
        useSessionStore.getState().addSession({
          key: sessionKey,
          agentId,
          title: text.trim().slice(0, 40) || 'New Chat',
          lastMessageAt: Date.now(),
          messageCount: 0,
        })
        useSessionStore.getState().setActiveSession(sessionKey)
      }

      addUserMessage(text)

      // Resolve attached files to server paths for chat.send:
      // - localPath files (pasted paths): pass directly, no upload needed
      // - File objects (picker/drag/paste): upload via HTTP first
      let media: { path: string; filename: string }[] | undefined
      if (attachedFiles?.length) {
        const api = getApiClient()
        const uploads = await Promise.all(
          attachedFiles.map(async (af) => {
            if (af.localPath) {
              return { path: af.localPath, filename: af.name }
            }
            if (!af.file) return null
            try {
              const res = await api.uploadFile<{ path: string; mime_type: string; filename: string }>(
                '/v1/media/upload', af.file,
              )
              return { path: res.path, filename: res.filename }
            } catch (err) {
              console.error('File upload failed:', af.name, err)
              return null
            }
          }),
        )
        media = uploads.filter((u): u is { path: string; filename: string } => u !== null)
        if (media.length === 0) media = undefined
      }

      try {
        await ws.call('chat.send', {
          message: text,
          agentId,
          sessionKey,
          stream: true,
          ...(media && { media }),
        })
      } catch (err) {
        console.error('chat.send failed:', err)
      }
    },
    [ws, activeSessionKey, addUserMessage],
  )

  const loadHistory = useCallback(
    async (sessionKey: string) => {
      if (!ws) return
      try {
        const result = (await ws.call('chat.history', { sessionKey })) as {
          messages?: Array<{
            id?: string
            role: string
            content?: string
            thinking?: string
            timestamp?: number
            tool_call_id?: string
            is_error?: boolean
            tool_calls?: Array<{
              id: string
              function?: { name: string; arguments?: Record<string, unknown> }
              name?: string
              input?: Record<string, unknown>
            }>
            media_refs?: Array<{
              id?: string
              mime_type?: string
              content_type?: string
              kind?: string
              path?: string
              url?: string
            }>
          }>
        }
        if (result?.messages) {
          // Build tool result map for enriching assistant tool_calls
          const toolResultMap = new Map<string, { content: string; isError: boolean }>()
          for (const m of result.messages) {
            if (m.role === 'tool' && m.tool_call_id) {
              toolResultMap.set(m.tool_call_id, {
                content: m.content ?? '',
                isError: !!m.is_error,
              })
            }
          }

          // Filter: only user + assistant messages, exclude internal system nudges
          const filtered = result.messages.filter((m) =>
            (m.role === 'user' || m.role === 'assistant') &&
            !(m.role === 'user' && m.content?.startsWith('[System]'))
          )
          setMessages(
            filtered.map((m) => ({
              id: m.id ?? crypto.randomUUID(),
              role: m.role as 'user' | 'assistant',
              content: m.content ?? '',
              timestamp: m.timestamp ?? Date.now(),
              thinkingText: m.thinking,
              toolCalls: m.tool_calls?.map((tc) => {
                const toolResult = toolResultMap.get(tc.id)
                return {
                  toolId: tc.id,
                  toolName: tc.function?.name ?? tc.name ?? 'unknown',
                  arguments: tc.function?.arguments ?? tc.input ?? {},
                  state: (toolResult?.isError ? 'error' : 'completed') as 'error' | 'completed',
                  result: toolResult && !toolResult.isError ? toolResult.content : undefined,
                  error: toolResult?.isError ? toolResult.content : undefined,
                }
              }),
              media: m.media_refs?.map((ref) => ({
                type: ref.mime_type ?? ref.content_type ?? 'image',
                url: toFileUrl(ref.path ?? ref.id ?? ref.url ?? ''),
              })),
            })),
          )
        }
      } catch (err) {
        console.error('Failed to load history:', err)
      }
    },
    [ws, setMessages],
  )

  // Abort the current run for the active session.
  const abort = useCallback(async () => {
    const sk = sessionKeyRef.current
    if (!ws || !sk) return
    try {
      await ws.call('chat.abort', { sessionKey: sk })
    } catch {
      // ignore abort errors
    }
  }, [ws])

  // Reset streaming state + load history when session changes.
  // Messages are NOT cleared here — loadHistory() replaces them atomically.
  // Clearing would race with sendMessage's auto-session-creation flow.
  const prevSessionRef = useRef<string | null>(null)
  useEffect(() => {
    if (activeSessionKey === prevSessionRef.current) return
    prevSessionRef.current = activeSessionKey

    // Reset streaming state only (not messages)
    chunkBatcher.flush()
    thinkingBatcher.flush()
    currentRunIdRef.current = null

    if (!activeSessionKey) {
      useChatStore.getState().clear()
      return
    }

    // Load history — setMessages() atomically replaces, no flash
    let cancelled = false
    loadHistory(activeSessionKey).then(() => {
      if (cancelled) return
    })

    // Restore session running state (mirrors web UI pattern).
    // If user switches to a session with an active run, show the stop button.
    ws?.call('chat.session_status', { sessionKey: activeSessionKey })
      .then((raw: unknown) => {
        if (cancelled) return
        const res = raw as { isRunning?: boolean; activity?: { phase: string; tool?: string; iteration?: number } }
        if (res?.isRunning) {
          useChatStore.getState().restoreRunning(res.activity ?? null)
        }
      })
      .catch(() => {})

    return () => { cancelled = true }
  }, [activeSessionKey, ws, loadHistory, chunkBatcher, thinkingBatcher])

  return {
    messages,
    isRunning,
    activity,
    sendMessage,
    loadHistory,
    abort,
  }
}

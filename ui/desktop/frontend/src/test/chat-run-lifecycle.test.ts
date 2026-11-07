import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act } from '@testing-library/react'

type Handler = (raw: unknown) => void
type ConnState = { status: string; nextRetryAt: number | null; lost: boolean }
type ConnListener = (s: ConnState) => void

const h = vi.hoisted(() => ({
  agentHandlers: new Set<(raw: unknown) => void>(),
  connListeners: new Set<(s: { status: string; nextRetryAt: number | null; lost: boolean }) => void>(),
  status: { isRunning: false } as { isRunning: boolean; activity?: { phase: string } },
  history: [] as unknown[],
  call: vi.fn(),
}))

vi.mock('../lib/ws', () => {
  const client = {
    call: (...args: unknown[]) => h.call(...args),
    on: (_event: string, handler: Handler) => { h.agentHandlers.add(handler); return () => h.agentHandlers.delete(handler) },
    connectionState: { status: 'online', nextRetryAt: null, lost: false },
    subscribeConnection: (l: ConnListener) => { h.connListeners.add(l); return () => h.connListeners.delete(l) },
    retryNow: () => undefined,
  }
  return { getWsClient: () => client }
})

import { useChatEvents } from '../hooks/use-chat-events'
import { useChat } from '../hooks/use-chat'
import { useChatMessageStore } from '../stores/chat-message-store'
import { useChatActivityStore } from '../stores/chat-activity-store'
import { useSessionStore } from '../stores/session-store'

const emit = (type: string, runId: string, payload: Record<string, unknown> = {}, sessionKey = 's1') =>
  act(() => { h.agentHandlers.forEach((fn) => fn({ type, runId, sessionKey, payload })) })

const setConnection = (lost: boolean) =>
  act(() => { h.connListeners.forEach((l) => l({ status: lost ? 'offline' : 'online', nextRetryAt: null, lost })) })

const flush = () => act(async () => { await new Promise((r) => setTimeout(r, 30)) })

beforeEach(() => {
  h.agentHandlers.clear()
  h.connListeners.clear()
  h.status = { isRunning: false }
  h.history = []
  h.call.mockReset()
  h.call.mockImplementation(async (method: string) => {
    if (method === 'chat.session_status') return h.status
    if (method === 'chat.history') return { messages: h.history }
    return {}
  })
  useChatMessageStore.getState().clear()
  useChatActivityStore.getState().clear()
  useSessionStore.setState({ activeSessionKey: 's1' })
})

describe('chat events live at shell level', () => {
  it('keeps the run going while the chat screen is unmounted', async () => {
    const shell = renderHook(() => useChatEvents())
    const screen = renderHook(() => useChat())
    await flush()
    emit('run.started', 'r1')
    screen.unmount() // user left the chat screen
    emit('chunk', 'r1', { content: 'Hello' })
    emit('run.completed', 'r1', { content: 'Hello world' })
    await flush()

    expect(useChatActivityStore.getState().isRunning).toBe(false)
    const reply = useChatMessageStore.getState().messages.find((m) => m.id === 'r1')
    expect(reply?.content).toBe('Hello world')
    shell.unmount()
  })

  it('reloads history when a run finishes that this client never saw start', async () => {
    useChatMessageStore.getState().setMessages([{ id: 'old', role: 'assistant', content: 'earlier', timestamp: 1 } as never])
    renderHook(() => useChatEvents())
    await flush()
    useChatMessageStore.getState().setMessages([{ id: 'old', role: 'assistant', content: 'earlier', timestamp: 1 } as never])
    useChatActivityStore.getState().restoreRunning(null)
    h.history = [{ role: 'assistant', content: 'final answer', timestamp: 2 }]

    emit('run.completed', 'unknown-run', { content: 'final answer' })
    await flush()

    expect(useChatActivityStore.getState().isRunning).toBe(false)
    expect(h.call).toHaveBeenCalledWith('chat.history', { sessionKey: 's1' })
    expect(useChatMessageStore.getState().messages.some((m) => m.content === 'earlier')).toBe(false)
  })

  it('does not append chunks of an unseen run to the last history message', async () => {
    renderHook(() => useChatEvents())
    await flush()
    useChatMessageStore.getState().setMessages([{ id: 'old', role: 'assistant', content: 'earlier', timestamp: 1 } as never])
    emit('chunk', 'r9', { content: 'new' })
    await flush()
    const messages = useChatMessageStore.getState().messages
    expect(messages.find((m) => m.id === 'old')?.content).toBe('earlier')
    expect(messages.some((m) => m.id === 'r9')).toBe(true)
  })

  it('ignores events from other conversations', async () => {
    renderHook(() => useChatEvents())
    await flush()
    emit('run.started', 'rx', {}, 'someone-else')
    expect(useChatActivityStore.getState().isRunning).toBe(false)
  })

  it('clears a stuck running flag when the gateway says nothing is running', async () => {
    useSessionStore.setState({ activeSessionKey: null })
    renderHook(() => useChatEvents())
    await flush()
    useChatActivityStore.getState().restoreRunning(null)
    act(() => useSessionStore.setState({ activeSessionKey: 's1' }))
    await flush()
    expect(useChatActivityStore.getState().isRunning).toBe(false)
  })
})

describe('connection loss', () => {
  it('ends the run with an interrupted notice, then restores it if the gateway is still running it', async () => {
    renderHook(() => useChatEvents())
    await flush()
    emit('run.started', 'r1')
    expect(useChatActivityStore.getState().isRunning).toBe(true)

    setConnection(true)
    expect(useChatActivityStore.getState().isRunning).toBe(false)
    expect(useChatActivityStore.getState().error?.kind).toBe('interrupted')

    h.status = { isRunning: true, activity: { phase: 'thinking' } }
    setConnection(false)
    await flush()
    expect(useChatActivityStore.getState().isRunning).toBe(true)
    expect(useChatActivityStore.getState().error).toBeNull()
  })

  it('reloads the saved answer after reconnect when the run already finished', async () => {
    renderHook(() => useChatEvents())
    await flush()
    emit('run.started', 'r1')
    setConnection(true)
    h.history = [{ role: 'assistant', content: 'done while away', timestamp: 3 }]
    setConnection(false)
    await flush()
    expect(useChatActivityStore.getState().isRunning).toBe(false)
    expect(useChatMessageStore.getState().messages.some((m) => m.content === 'done while away')).toBe(true)
  })
})

describe('retry', () => {
  it('resends the last request with its attachments and does not duplicate the user message', async () => {
    const { result } = renderHook(() => useChat())
    const file = { name: 'a.png', localPath: '/tmp/a.png' } as never
    await act(async () => { await result.current.sendMessage('look at this', 'agent-1', [file]) })
    h.call.mockClear()

    await act(async () => { await result.current.retry('agent-1') })

    const send = h.call.mock.calls.find(([m]) => m === 'chat.send')
    expect(send?.[1]).toMatchObject({ message: 'look at this', media: [{ path: '/tmp/a.png', filename: 'a.png' }] })
    expect(useChatMessageStore.getState().messages.filter((m) => m.role === 'user')).toHaveLength(1)
  })

  it('sends the continue prompt as a translated string', async () => {
    const { result } = renderHook(() => useChat())
    await act(async () => { await result.current.continueReply('agent-1') })
    const send = h.call.mock.calls.find(([m]) => m === 'chat.send')
    const message = (send?.[1] as { message: string }).message
    expect(message.length).toBeGreaterThan(0)
    expect(message).not.toContain('notice.')
  })
})

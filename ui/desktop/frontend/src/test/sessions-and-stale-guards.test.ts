import { describe, it, expect, vi, beforeEach } from 'vitest'
import { renderHook, act, waitFor } from '@testing-library/react'

const h = vi.hoisted(() => ({
  list: vi.fn(),
  overview: vi.fn(),
  get: vi.fn(),
}))

vi.mock('../services/session-service', () => ({
  sessionService: { list: (...a: unknown[]) => h.list(...a), rename: vi.fn(), delete: vi.fn() },
}))
vi.mock('../services/usage-service', () => ({ usageService: { overview: (...a: unknown[]) => h.overview(...a) } }))
vi.mock('../lib/api', () => ({ isApiClientReady: () => true, getApiClient: () => ({ get: (...a: unknown[]) => h.get(...a) }) }))

import { useAgentSessions } from '../hooks/use-agent-sessions'
import { useSessions } from '../hooks/use-sessions'
import { useUsageOverview } from '../hooks/use-usage-overview'
import { useTraces } from '../hooks/use-traces'
import { useSessionStore } from '../stores/session-store'
import { useAgentStore } from '../stores/agent-store'

function deferred<T>() {
  let resolve!: (v: T) => void
  const promise = new Promise<T>((r) => { resolve = r })
  return { promise, resolve }
}

beforeEach(() => {
  h.list.mockReset()
  h.overview.mockReset()
  h.get.mockReset()
  useSessionStore.setState({ sessions: [], activeSessionKey: 'open-chat', status: 'idle', error: null })
  useAgentStore.setState({ selectedAgentId: null } as never)
})

describe('sessions without an agent', () => {
  it('reports a loaded, empty list instead of staying idle', async () => {
    renderHook(() => useSessions())
    await waitFor(() => expect(useSessionStore.getState().status).toBe('ready'))
    expect(useSessionStore.getState().sessions).toEqual([])
  })
})

describe('sessions view agent filter', () => {
  it('lists another agent without touching the selected agent or the open chat', async () => {
    h.list.mockResolvedValue({
      sessions: [{ key: 'k1', label: 'One', messageCount: 2, created: '2026-01-01T00:00:00Z', updated: '2026-01-02T00:00:00Z' }],
    })
    useAgentStore.setState({ selectedAgentId: 'a1' } as never)
    const { result } = renderHook(() => useAgentSessions('a2'))
    await waitFor(() => expect(result.current.status).toBe('ready'))

    expect(h.list).toHaveBeenCalledWith('a2')
    expect(result.current.sessions.map((s) => s.key)).toEqual(['k1'])
    expect(useAgentStore.getState().selectedAgentId).toBe('a1')
    expect(useSessionStore.getState().activeSessionKey).toBe('open-chat')
  })

  it('drops a slow reply for the previous agent', async () => {
    const slow = deferred<{ sessions: unknown[] }>()
    h.list.mockImplementation((agentId: string) =>
      agentId === 'a1' ? slow.promise : Promise.resolve({ sessions: [{ key: 'b', label: 'B', created: '2026-01-01T00:00:00Z' }] }))
    const { result, rerender } = renderHook(({ id }) => useAgentSessions(id), { initialProps: { id: 'a1' } })
    rerender({ id: 'a2' })
    await waitFor(() => expect(result.current.sessions.map((s) => s.key)).toEqual(['b']))
    await act(async () => {
      slow.resolve({ sessions: [{ key: 'a', label: 'A', created: '2026-01-01T00:00:00Z' }] })
      await slow.promise
    })
    expect(result.current.sessions.map((s) => s.key)).toEqual(['b'])
  })
})

describe('stale responses', () => {
  it('usage overview ignores a reply for an older range', async () => {
    const slow = deferred<{ range: string }>()
    h.overview.mockImplementation((range: string) => (range === '24h' ? slow.promise : Promise.resolve({ range })))
    const { result, rerender } = renderHook(({ range }) => useUsageOverview(range as never), { initialProps: { range: '24h' } })
    rerender({ range: '7d' })
    await waitFor(() => expect(result.current.data).toEqual({ range: '7d' }))
    await act(async () => { slow.resolve({ range: '24h' }); await slow.promise })
    expect(result.current.data).toEqual({ range: '7d' })
    expect(result.current.loading).toBe(false)
  })

  it('traces: load more for the old filter does not mix into the new filter', async () => {
    const page = (id: string) => ({ traces: [{ id }], total: 40 })
    const slowMore = deferred<ReturnType<typeof page>>()
    h.get.mockImplementation((url: string) => {
      if (url.includes('offset=20')) return slowMore.promise
      return Promise.resolve(url.includes('agent_id=b') ? page('b-first') : page('all-first'))
    })
    const { result } = renderHook(() => useTraces())
    await waitFor(() => expect(result.current.traces.map((t) => t.id)).toEqual(['all-first']))

    let more!: Promise<void>
    act(() => { more = result.current.loadMore() })
    act(() => result.current.setAgentFilter('b'))
    await waitFor(() => expect(result.current.traces.map((t) => t.id)).toEqual(['b-first']))
    await act(async () => { slowMore.resolve(page('all-second')); await more })

    expect(result.current.traces.map((t) => t.id)).toEqual(['b-first'])
    expect(result.current.loadingMore).toBe(false)
  })
})

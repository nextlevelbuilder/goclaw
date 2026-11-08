import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, renderHook } from '@testing-library/react'
import { initWsClient, type WsClient } from '../../../lib/ws'
import { useGatewayStatus } from '../use-gateway-status'

/** Minimal WebSocket stand-in the client can drive. */
class FakeSocket {
  static instances: FakeSocket[] = []
  onopen: (() => void) | null = null
  onmessage: ((e: { data: string }) => void) | null = null
  onclose: ((e: { code: number }) => void) | null = null
  onerror: (() => void) | null = null
  sent: Array<{ id: string; method: string }> = []
  constructor(public url: string) { FakeSocket.instances.push(this) }
  send(raw: string) { this.sent.push(JSON.parse(raw)) }
  close() { this.onclose?.({ code: 1000 }) }
  /** Open and acknowledge the connect handshake. */
  accept() {
    this.onopen?.()
    const req = this.sent.find((f) => f.method === 'connect')!
    this.onmessage?.({ data: JSON.stringify({ type: 'res', id: req.id, ok: true, payload: {} }) })
  }
  drop() { this.onclose?.({ code: 1006 }) }
}

let client: WsClient

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-11-07T08:00:00Z'))
  FakeSocket.instances = []
  vi.stubGlobal('WebSocket', FakeSocket)
  vi.spyOn(console, 'info').mockImplementation(() => {})
  client = initWsClient('ws://gateway.test/ws', 'token')
})

afterEach(() => {
  client.close()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  vi.useRealTimers()
})

const socket = () => FakeSocket.instances[FakeSocket.instances.length - 1]

describe('WsClient connection state', () => {
  it('goes connecting → online → offline with a scheduled retry', () => {
    const seen: string[] = []
    client.subscribeConnection((s) => seen.push(s.status))
    expect(client.connectionState.status).toBe('connecting')
    socket().accept()
    expect(client.connectionState).toEqual({ status: 'online', nextRetryAt: null, lost: false })
    socket().drop()
    expect(client.connectionState.status).toBe('offline')
    expect(client.connectionState.nextRetryAt).toBe(Date.now() + 1000)
    expect(seen).toEqual(['online', 'offline'])
  })

  it('reconnects on its own after the backoff', () => {
    socket().drop()
    vi.advanceTimersByTime(1000)
    expect(client.connectionState.status).toBe('connecting')
    expect(FakeSocket.instances).toHaveLength(2)
    socket().accept()
    expect(client.connectionState.status).toBe('online')
  })

  it('retryNow skips the wait', () => {
    socket().drop()
    client.retryNow()
    expect(client.connectionState.status).toBe('connecting')
    expect(FakeSocket.instances).toHaveLength(2)
  })

  it('keeps the legacy boolean listener working', () => {
    const onChange = vi.fn()
    const off = client.onConnectionChange(onChange)
    socket().accept()
    expect(onChange).toHaveBeenLastCalledWith(true)
    off()
    socket().drop()
    expect(onChange).toHaveBeenCalledTimes(1)
  })
})

describe('useGatewayStatus', () => {
  it('reports online and offline with a live countdown, and retries now', () => {
    const { result } = renderHook(() => useGatewayStatus())
    expect(result.current).toMatchObject({ status: 'connecting', disconnected: false })

    act(() => { socket().accept() })
    expect(result.current).toMatchObject({ status: 'online', online: true, disconnected: false, retryInSeconds: null })

    act(() => { socket().drop() })
    // Second failure in a row doubles the backoff to 2s.
    act(() => { vi.advanceTimersByTime(1000) })
    act(() => { socket().drop() })
    expect(result.current).toMatchObject({ status: 'offline', online: false, disconnected: true, retryInSeconds: 2 })
    act(() => { vi.advanceTimersByTime(1000) })
    expect(result.current.retryInSeconds).toBe(1)

    act(() => { result.current.retryNow() })
    // Still disconnected while the retry is in flight, so the banner does not flicker.
    expect(result.current).toMatchObject({ status: 'connecting', disconnected: true, retryInSeconds: null })
    act(() => { socket().accept() })
    expect(result.current).toMatchObject({ online: true, disconnected: false })
  })
})

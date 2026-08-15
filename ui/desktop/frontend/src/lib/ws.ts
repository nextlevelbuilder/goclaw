// WebSocket v3 client for GoClaw gateway RPC protocol

import type { Frame, ResponseFrame, EventFrame, EventHandler, PendingRequest } from './ws-types'
import {
  DEFAULT_TIMEOUT_MS,
  CHAT_SEND_TIMEOUT_MS,
  RECONNECT_BASE_MS,
  RECONNECT_MAX_MS,
} from './ws-types'

export type { FrameType, RequestFrame, ResponseFrame, EventFrame, Frame, EventHandler } from './ws-types'

export class WsClient {
  private ws: WebSocket | null = null
  private url: string
  private token: string
  private connected = false
  private connecting = false
  private pendingRequests = new Map<string, PendingRequest>()
  private eventHandlers = new Map<string, Set<EventHandler>>()
  private reconnectDelay = RECONNECT_BASE_MS
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null
  private connectionChangeHandler?: (connected: boolean) => void
  private closed = false
  private queuedCalls: Array<() => void> = []
  private connectRequestId: string | null = null

  constructor(url: string, token: string) {
    this.url = url
    this.token = token
  }

  connect(): void {
    if (this.closed || this.connecting || this.connected) return
    this.connecting = true

    console.info('[ws] connecting to', this.url)
    this.ws = new WebSocket(this.url)
    this.ws.onopen = () => this.handleOpen()
    this.ws.onmessage = (e) => this.handleMessage(e.data as string)
    this.ws.onclose = (e) => this.handleClose(e)
    this.ws.onerror = () => console.warn('[ws] socket error')
  }

  private handleOpen(): void {
    this.connecting = false
    const id = this.nextId()
    this.connectRequestId = id
    this.sendRaw({
      type: 'req',
      id,
      method: 'connect',
      params: {
        token: this.token,
        user_id: 'system',
        sender_id: 'desktop',
        locale: localStorage.getItem('goclaw:language') || navigator.language.split('-')[0] || 'en',
        protocol_version: 3,
      },
    })
  }

  private handleMessage(data: string): void {
    let frame: Frame
    try {
      frame = JSON.parse(data) as Frame
    } catch {
      console.warn('[ws] invalid frame', data)
      return
    }

    if (frame.type === 'res') {
      this.handleResponse(frame)
    } else if (frame.type === 'event') {
      this.handleEvent(frame)
    }
  }

  private handleResponse(frame: ResponseFrame): void {
    if (this.connectRequestId && frame.id === this.connectRequestId) {
      this.connectRequestId = null
      if (frame.ok) {
        this.onSessionConnected()
      } else {
        console.error('[ws] connect handshake failed', frame.error)
      }
      return
    }

    const pending = this.pendingRequests.get(frame.id)
    if (!pending) return

    clearTimeout(pending.timer)
    this.pendingRequests.delete(frame.id)

    if (frame.ok) {
      pending.resolve(frame.payload)
    } else {
      const err = frame.error
      const msg = err?.message ?? 'RPC error'
      pending.reject(Object.assign(new Error(msg), { code: err?.code, retryable: err?.retryable }))
    }
  }

  private handleEvent(frame: EventFrame): void {
    const handlers = this.eventHandlers.get(frame.event)
    if (!handlers) return
    for (const h of handlers) {
      try {
        h(frame.payload)
      } catch (err) {
        console.error('[ws] event handler error', frame.event, err)
      }
    }
  }

  private onSessionConnected(): void {
    console.info('[ws] session connected')
    this.connected = true
    this.reconnectDelay = RECONNECT_BASE_MS
    this.connectionChangeHandler?.(true)

    const queued = this.queuedCalls.splice(0)
    for (const fn of queued) fn()
  }

  private handleClose(e: CloseEvent): void {
    const wasConnected = this.connected
    this.connected = false
    this.connecting = false
    this.ws = null

    if (wasConnected) this.connectionChangeHandler?.(false)

    for (const [, pending] of this.pendingRequests) {
      clearTimeout(pending.timer)
      pending.reject(new Error('WebSocket disconnected'))
    }
    this.pendingRequests.clear()

    if (!this.closed) {
      console.info(`[ws] closed (code=${e.code}), reconnecting in ${this.reconnectDelay}ms`)
      this.scheduleReconnect()
    }
  }

  call(method: string, params?: Record<string, unknown>, timeoutMs?: number): Promise<unknown> {
    const timeout = timeoutMs ?? (method === 'chat.send' ? CHAT_SEND_TIMEOUT_MS : DEFAULT_TIMEOUT_MS)

    if (!this.connected) {
      return new Promise((resolve, reject) => {
        this.queuedCalls.push(() => {
          this.call(method, params, timeoutMs).then(resolve, reject)
        })
      })
    }

    return new Promise((resolve, reject) => {
      const id = this.nextId()
      const timer = setTimeout(() => {
        this.pendingRequests.delete(id)
        reject(new Error(`RPC timeout: ${method}`))
      }, timeout)

      this.pendingRequests.set(id, { resolve, reject, timer, timeoutMs: timeout })
      this.sendRaw({ type: 'req', id, method, params })
    })
  }

  resetTimeout(requestId: string, timeoutMs?: number): void {
    const pending = this.pendingRequests.get(requestId)
    if (!pending) return
    clearTimeout(pending.timer)
    const ms = timeoutMs ?? pending.timeoutMs
    pending.timer = setTimeout(() => {
      this.pendingRequests.delete(requestId)
      pending.reject(new Error('RPC streaming timeout'))
    }, ms)
  }

  on(event: string, handler: EventHandler): () => void {
    let handlers = this.eventHandlers.get(event)
    if (!handlers) {
      handlers = new Set()
      this.eventHandlers.set(event, handlers)
    }
    handlers.add(handler)
    return () => {
      handlers!.delete(handler)
      if (handlers!.size === 0) this.eventHandlers.delete(event)
    }
  }

  onConnectionChange(handler: (connected: boolean) => void): void {
    this.connectionChangeHandler = handler
  }

  close(): void {
    this.closed = true
    if (this.reconnectTimer) clearTimeout(this.reconnectTimer)
    this.ws?.close()
  }

  private scheduleReconnect(): void {
    this.reconnectTimer = setTimeout(() => {
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, RECONNECT_MAX_MS)
      this.connect()
    }, this.reconnectDelay)
  }

  private sendRaw(frame: Frame): void {
    this.ws?.send(JSON.stringify(frame))
  }

  private nextId(): string {
    return crypto.randomUUID()
  }

  get isConnected(): boolean {
    return this.connected
  }
}

// Singleton
let client: WsClient | null = null

export function getWsClient(): WsClient {
  if (!client) throw new Error('WsClient not initialized — call initWsClient() first')
  return client
}

export function initWsClient(url: string, token: string): WsClient {
  if (client) client.close()
  client = new WsClient(url, token)
  client.connect()
  return client
}

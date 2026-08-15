// WebSocket v3 protocol type definitions

export type FrameType = 'req' | 'res' | 'event'

export interface RequestFrame {
  type: 'req'
  id: string
  method: string
  params?: Record<string, unknown>
}

export interface ResponseFrame {
  type: 'res'
  id: string
  ok: boolean
  payload?: unknown
  error?: {
    code: string
    message: string
    details?: unknown
    retryable?: boolean
    retryAfterMs?: number
  }
}

export interface EventFrame {
  type: 'event'
  event: string
  payload: unknown
  seq?: number
}

export type Frame = RequestFrame | ResponseFrame | EventFrame

export type EventHandler = (payload: unknown) => void

export interface PendingRequest {
  resolve: (payload: unknown) => void
  reject: (error: Error) => void
  timer: ReturnType<typeof setTimeout>
  timeoutMs: number
}

export const DEFAULT_TIMEOUT_MS = 30_000
export const CHAT_SEND_TIMEOUT_MS = 600_000
export const RECONNECT_BASE_MS = 1_000
export const RECONNECT_MAX_MS = 30_000

import { describe, expect, it } from 'vitest'
import { classifyRunError, isToolDenied } from '../utils/chat-error'
import { dayGroup, relativeTime } from '../utils/chat-time'
import { useChatActivityStore } from '../stores/chat-activity-store'
import { useSessionStore } from '../stores/session-store'

describe('chat error classification', () => {
  it('maps rate-limit and cooldown text to provider', () => {
    expect(classifyRunError('provider is cooling down, 429 rate limit')).toBe('provider')
  })
  it('falls back to failed for unknown text', () => {
    expect(classifyRunError('something odd happened')).toBe('failed')
  })
  it('does not flag ordinary tool errors as denied', () => {
    expect(isToolDenied('file not found')).toBe(false)
    expect(isToolDenied(undefined)).toBe(false)
  })
})

describe('interrupted classification is limited to transport drops', () => {
  it.each([
    'websocket closed unexpectedly',
    'connection lost',
    'connection reset by peer',
    'unexpected EOF',
    'run interrupted',
  ])('treats "%s" as interrupted', (m) => {
    expect(classifyRunError(m)).toBe('interrupted')
  })
  it.each([
    'dial tcp: connection refused',
    'stream error: 401 unauthorized',
    'request timeout after 30s',
    'invalid api key',
  ])('treats "%s" as an ordinary failure', (m) => {
    expect(classifyRunError(m)).toBe('failed')
  })
})

describe('chat time helpers', () => {
  const now = new Date(2026, 5, 15, 12, 0, 0).getTime()
  it('groups by calendar day', () => {
    expect(dayGroup(now - 60_000, now)).toBe('today')
    expect(dayGroup(now - 24 * 3600_000, now)).toBe('yesterday')
    expect(dayGroup(now - 5 * 24 * 3600_000, now)).toBe('earlier')
  })
  it('formats relative time per locale', () => {
    expect(relativeTime(now - 5 * 60_000, 'en', now)).toMatch(/5/)
  })
})

describe('chat activity error state', () => {
  it('failRun stores the error and startRun clears it', () => {
    const s = useChatActivityStore.getState()
    s.failRun({ kind: 'provider', message: 'x' })
    expect(useChatActivityStore.getState().error?.kind).toBe('provider')
    useChatActivityStore.getState().startRun('r1')
    expect(useChatActivityStore.getState().error).toBeNull()
  })
})

describe('session store load state', () => {
  it('setSessions marks the list ready and reload bumps the nonce', () => {
    const before = useSessionStore.getState().reloadNonce
    useSessionStore.getState().setStatus('error', 'boom')
    expect(useSessionStore.getState().status).toBe('error')
    useSessionStore.getState().setSessions([])
    expect(useSessionStore.getState().status).toBe('ready')
    useSessionStore.getState().reload()
    expect(useSessionStore.getState().reloadNonce).toBe(before + 1)
  })
})

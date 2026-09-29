import { describe, it, expect } from 'vitest'
import { buildContextPruningPayload, isContextPruningEnabled } from '../context-pruning'

describe('isContextPruningEnabled', () => {
  it('treats an unset config as the backend default (enabled)', () => {
    expect(isContextPruningEnabled(null)).toBe(true)
    expect(isContextPruningEnabled(undefined)).toBe(true)
    expect(isContextPruningEnabled({ keepLastAssistants: 5 })).toBe(true)
  })

  it('is disabled only for an explicit off mode', () => {
    expect(isContextPruningEnabled({ mode: 'off' })).toBe(false)
    expect(isContextPruningEnabled({ mode: 'cache-ttl' })).toBe(true)
  })
})

describe('buildContextPruningPayload', () => {
  it('leaves an inherited (unset) config unset when the user did not change it', () => {
    expect(buildContextPruningPayload(null, true, {})).toBeUndefined()
  })

  it('saves an explicit off mode when disabled, since null means enabled', () => {
    expect(buildContextPruningPayload(null, false, { keepLastAssistants: 5 })).toEqual({ mode: 'off' })
  })

  it('keeps settings and forces cache-ttl mode when re-enabled', () => {
    expect(buildContextPruningPayload({ mode: 'off' }, true, { mode: 'off', keepLastAssistants: 5 })).toEqual({
      mode: 'cache-ttl',
      keepLastAssistants: 5,
    })
  })
})

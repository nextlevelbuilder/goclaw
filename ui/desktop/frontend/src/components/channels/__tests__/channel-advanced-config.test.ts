import { describe, expect, it } from 'vitest'
import { buildAdvancedConfigUpdate, deriveAdvancedInitialValues } from '../channel-advanced-config'

describe('channel advanced config payload', () => {
  it('removes a stale allow_from entry when the allowed users field is cleared', () => {
    const existing = {
      dm_policy: 'allowlist',
      group_policy: 'open',
      allow_from: ['195835936795841454', '@alice'],
      groups: { '-100123': { allow_from: ['group-user'] } },
    }

    const values = deriveAdvancedInitialValues(existing)
    const next = buildAdvancedConfigUpdate(existing, { ...values, allow_from: undefined })

    expect(next).not.toHaveProperty('allow_from')
    expect(next.dm_policy).toBe('allowlist')
    expect(next.group_policy).toBe('open')
    expect(next.groups).toEqual(existing.groups)
  })

  it('removes cleared text fields and keeps edited ones', () => {
    const existing = { proxy: 'http://localhost:8080', history_limit: 50, allow_from: ['1'] }

    const next = buildAdvancedConfigUpdate(existing, { proxy: '', history_limit: 20, allow_from: ['1', '2'] })

    expect(next).not.toHaveProperty('proxy')
    expect(next.history_limit).toBe(20)
    expect(next.allow_from).toEqual(['1', '2'])
  })

  it('never lets advanced values overwrite essential keys or group overrides', () => {
    const existing = { dm_policy: 'pairing', groups: { g: { enabled: true } } }

    const next = buildAdvancedConfigUpdate(existing, { dm_policy: undefined, groups: undefined, link_preview: false })

    expect(next).toEqual({ dm_policy: 'pairing', groups: { g: { enabled: true } }, link_preview: false })
  })

  it('excludes essential keys and group overrides from the editable values', () => {
    const values = deriveAdvancedInitialValues({
      dm_policy: 'pairing',
      require_mention: true,
      groups: { g: {} },
      allow_from: ['1'],
      reasoning_stream: false,
    })

    expect(values).toEqual({ allow_from: ['1'], reasoning_delivery: 'off' })
  })
})

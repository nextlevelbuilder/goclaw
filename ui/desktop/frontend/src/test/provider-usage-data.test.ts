import { describe, it, expect, vi, beforeEach } from 'vitest'
import healthFixture from './fixtures/provider-health.json'
import usageDetailFixture from './fixtures/provider-usage-detail.json'

const { getWithParams, get } = vi.hoisted(() => ({ getWithParams: vi.fn(), get: vi.fn() }))
vi.mock('../lib/api', () => ({ getApiClient: () => ({ get, getWithParams }) }))

import {
  parseProviderHealth,
  parseProviderUsageDetail,
  parseUsageSummary,
  parseUsageTimeSeries,
  rangeWindow,
  usageService,
} from '../services/usage-service'
import {
  cooldownMinutesLeft,
  dailyTokenBars,
  errorClassKey,
  errorRate,
  formatLatency,
  providerStatus,
  requestsPerDay,
} from '../components/providers/provider-usage-format'
import { PROVIDER_ERROR_CLASSES } from '../types/provider'
import enProviders from '../i18n/locales/en/providers.json'

describe('provider health fixture (real handler output)', () => {
  it('parses every field', () => {
    const h = parseProviderHealth(healthFixture)
    expect(h).toEqual({
      provider_id: '0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b',
      provider: 'dashscope',
      enabled: true,
      consecutive_failures: 3,
      cooling_down: true,
      cooldown_until: '2026-11-06T04:42:00Z',
      last_error_class: 'rate_limit',
      error_counts: { rate_limit: 14, timeout: 2 },
      last_probe_at: null,
      updated_at: '2026-11-06T04:40:30Z',
      max_cooldown_seconds: 3600,
    })
  })

  it('rejects malformed payloads', () => {
    expect(() => parseProviderHealth(null)).toThrow()
    expect(() => parseProviderHealth([])).toThrow()
    expect(() => parseProviderHealth({ ...healthFixture, cooling_down: 'yes' })).toThrow(/cooling_down/)
    expect(() => parseProviderHealth({ ...healthFixture, cooldown_until: 'not a time' })).toThrow(/cooldown_until/)
  })

  it('treats a healthy provider with no history as not cooling down', () => {
    const h = parseProviderHealth({
      ...healthFixture,
      cooling_down: false,
      cooldown_until: null,
      last_error_class: '',
      error_counts: null,
      updated_at: null,
    })
    expect(h.cooling_down).toBe(false)
    expect(h.error_counts).toEqual({})
    expect(providerStatus(true, h)).toBe('running')
  })
})

describe('provider usage-detail fixture (real handler output)', () => {
  it('parses calls, latency and recent errors newest first', () => {
    const d = parseProviderUsageDetail(usageDetailFixture)
    expect(d.provider).toBe('dashscope')
    expect(d.calls).toBe(1284)
    expect(d.errors).toBe(80)
    expect(d.latency_ms).toEqual({ p50: 1400, p95: 4800 })
    expect(d.recent_errors.map((e) => e.error_class)).toEqual(['rate_limit', 'timeout', 'auth'])
    expect(d.recent_errors[0]).toEqual({ at: '2026-11-06T04:38:10Z', error_class: 'rate_limit', count: 2 })
  })

  it('drops malformed error items and rejects a missing latency block', () => {
    const d = parseProviderUsageDetail({
      ...usageDetailFixture,
      recent_errors: [{ at: 'bad', error_class: 'timeout', count: 1 }, null, { at: '2026-11-06T04:00:00Z', count: 3 }],
    })
    expect(d.recent_errors).toEqual([{ at: '2026-11-06T04:00:00Z', error_class: 'unknown', count: 3 }])
    expect(() => parseProviderUsageDetail({ ...usageDetailFixture, latency_ms: undefined })).toThrow(/latency_ms/)
    expect(() => parseProviderUsageDetail({ ...usageDetailFixture, calls: '12' })).toThrow(/calls/)
  })
})

describe('usage summary and timeseries parsing', () => {
  it('reads summary.current with json tags from internal/http/usage.go', () => {
    const s = parseUsageSummary({
      current: { requests: 10, input_tokens: 100, output_tokens: 50, cost: 0.25, unique_users: 1, errors: 1, llm_calls: 9, tool_calls: 2, avg_duration_ms: 900 },
      previous: {},
    })
    expect(s.current.requests).toBe(10)
    expect(s.current.cost).toBe(0.25)
    expect(s.previous.requests).toBe(0)
    expect(() => parseUsageSummary({})).toThrow(/current/)
  })

  it('reads points and skips ones without a valid bucket_time', () => {
    const pts = parseUsageTimeSeries({
      points: [
        { bucket_time: '2026-11-05T00:00:00Z', input_tokens: 10, output_tokens: 5, total_cost: 0.1, request_count: 2, error_count: 0 },
        { input_tokens: 99 },
      ],
    })
    expect(pts).toHaveLength(1)
    expect(pts[0].input_tokens).toBe(10)
    expect(parseUsageTimeSeries({ points: null })).toEqual([])
    expect(() => parseUsageTimeSeries({ points: {} })).toThrow()
  })
})

describe('usageService', () => {
  beforeEach(() => {
    get.mockReset()
    getWithParams.mockReset()
  })

  it('addresses usage-detail by id and the snapshot endpoints by provider name', async () => {
    getWithParams.mockImplementation(async (path: string) => {
      if (path.endsWith('/usage-detail')) return usageDetailFixture
      if (path === '/v1/usage/summary') return { current: { requests: 7, input_tokens: 3, output_tokens: 4, cost: 1 }, previous: {} }
      return { points: [] }
    })
    const now = new Date('2026-11-06T05:00:00.123Z')
    const b = await usageService.bundle('0199-id', 'dashscope', '7d', now)

    expect(b.detail.calls).toBe(1284)
    expect(b.summary.requests).toBe(7)
    const calls = Object.fromEntries(getWithParams.mock.calls.map(([p, q]) => [p, q]))
    expect(calls['/v1/providers/0199-id/usage-detail']).toEqual({
      from: '2026-10-30T05:00:00Z',
      to: '2026-11-06T05:00:00Z',
      limit: '20',
    })
    expect(calls['/v1/usage/summary']).toEqual({ period: '7d', provider: 'dashscope' })
    expect(calls['/v1/usage/timeseries']).toEqual({
      from: '2026-10-30T05:00:00Z',
      to: '2026-11-06T05:00:00Z',
      group_by: 'day',
      provider: 'dashscope',
    })
  })

  it('validates the health response', async () => {
    get.mockResolvedValue(healthFixture)
    await expect(usageService.health('0199-id')).resolves.toMatchObject({ cooling_down: true })
    expect(get).toHaveBeenCalledWith('/v1/providers/0199-id/health')
    get.mockResolvedValue({ nope: true })
    await expect(usageService.health('0199-id')).rejects.toThrow()
  })

  it('builds second-precision RFC3339 windows', () => {
    expect(rangeWindow('24h', new Date('2026-11-06T05:00:00.999Z'))).toEqual({
      from: '2026-11-05T05:00:00Z',
      to: '2026-11-06T05:00:00Z',
    })
  })
})

describe('usage helpers', () => {
  it('computes error rate from usage-detail errors / calls', () => {
    const d = parseProviderUsageDetail(usageDetailFixture)
    expect(errorRate(d.calls, d.errors)).toBeCloseTo(80 / 1284)
    expect(errorRate(0, 0)).toBeNull()
    expect(errorRate(10, 20)).toBe(1)
  })

  it('formats latency as ms below one second and seconds above', () => {
    expect(formatLatency(1400, 'en')).toEqual({ value: '1.4', unit: 'seconds' })
    expect(formatLatency(4800, 'en')).toEqual({ value: '4.8', unit: 'seconds' })
    expect(formatLatency(850.4, 'en')).toEqual({ value: '850', unit: 'milliseconds' })
    expect(formatLatency(Number.NaN, 'en')).toEqual({ value: '0', unit: 'milliseconds' })
  })

  it('maps error classes to i18n keys with an unknown fallback', () => {
    expect(errorClassKey('rate_limit')).toBe('usage.errorClass.rate_limit')
    expect(errorClassKey('auth_permanent')).toBe('usage.errorClass.auth_permanent')
    expect(errorClassKey('http_418')).toBe('usage.errorClass.unknown')
    expect(errorClassKey('')).toBe('usage.errorClass.unknown')
    expect(errorClassKey(undefined)).toBe('usage.errorClass.unknown')
  })

  it('has an en translation for every error class', () => {
    const classes = enProviders.usage.errorClass as Record<string, string>
    for (const cls of PROVIDER_ERROR_CLASSES) expect(classes[cls]).toBeTruthy()
  })

  it('derives the header status word', () => {
    const h = parseProviderHealth(healthFixture)
    expect(providerStatus(true, h)).toBe('rateLimited')
    expect(providerStatus(true, { ...h, last_error_class: 'overloaded' })).toBe('coolingDown')
    expect(providerStatus(false, h)).toBe('disabled')
    expect(providerStatus(true, null)).toBe('running')
  })

  it('rounds cooldown remaining time up to whole minutes', () => {
    const now = new Date('2026-11-06T04:38:30Z')
    expect(cooldownMinutesLeft('2026-11-06T04:42:00Z', now)).toBe(4)
    expect(cooldownMinutesLeft('2026-11-06T04:38:40Z', now)).toBe(1)
    expect(cooldownMinutesLeft('2026-11-06T04:00:00Z', now)).toBeNull()
    expect(cooldownMinutesLeft(null, now)).toBeNull()
  })

  it('averages requests per day over the range', () => {
    expect(requestsPerDay(1284, '7d')).toBe(183)
    expect(requestsPerDay(50, '24h')).toBe(50)
  })

  it('fills missing UTC days with zero tokens', () => {
    const now = new Date('2026-11-06T05:00:00Z')
    const bars = dailyTokenBars(
      [
        { bucket_time: '2026-11-04T00:00:00Z', input_tokens: 100, output_tokens: 50, total_cost: 0, request_count: 1, error_count: 0 },
        { bucket_time: '2026-11-06T00:00:00Z', input_tokens: 10, output_tokens: 0, total_cost: 0, request_count: 1, error_count: 0 },
      ],
      '7d',
      now,
    )
    expect(bars).toHaveLength(8)
    expect(bars[0]).toEqual({ day: '2026-10-30', tokens: 0 })
    expect(bars.find((b) => b.day === '2026-11-04')?.tokens).toBe(150)
    expect(bars[bars.length - 1]).toEqual({ day: '2026-11-06', tokens: 10 })
  })
})

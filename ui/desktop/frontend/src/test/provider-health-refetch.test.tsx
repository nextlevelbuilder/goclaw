import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { act, fireEvent, render, renderHook, screen } from '@testing-library/react'
import healthFixture from './fixtures/provider-health.json'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  getApiClient: () => ({ get, getWithParams: vi.fn() }),
}))

import i18n from '../i18n'
import { cooldownRecheckDelay, useProviderHealth } from '../hooks/use-provider-health'
import { ProviderNameCell } from '../components/providers/ProviderNameCell'
import { CooldownBanner } from '../components/providers/provider-usage-widgets'
import type { ProviderData } from '../types/provider'

const NOW = Date.parse('2026-11-06T05:00:00Z')
const iso = (offsetMs: number) => new Date(NOW + offsetMs).toISOString()
const cooling = (untilOffsetMs: number) => ({ ...healthFixture, cooling_down: true, cooldown_until: iso(untilOffsetMs) })
const healthy = { ...healthFixture, cooling_down: false, cooldown_until: null, consecutive_failures: 0, last_error_class: '' }

/** Advances fake time and flushes the resulting fetch promises inside act(). */
async function advance(ms: number) {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(ms)
  })
}

describe('cooldownRecheckDelay', () => {
  it('waits until the cooldown ends plus a grace period', () => {
    expect(cooldownRecheckDelay(iso(10_000), NOW)).toBe(11_500)
  })
  it('re-checks at most every 30 s', () => {
    expect(cooldownRecheckDelay(iso(10 * 60_000), NOW)).toBe(30_000)
    expect(cooldownRecheckDelay(null, NOW)).toBe(30_000)
    expect(cooldownRecheckDelay('not a date', NOW)).toBe(30_000)
  })
  it('waits 5 s when the cooldown should already be over', () => {
    expect(cooldownRecheckDelay(iso(-60_000), NOW)).toBe(5_000)
    expect(cooldownRecheckDelay(iso(-1_500), NOW)).toBe(5_000)
  })
})

describe('useProviderHealth re-check timer', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(NOW)
    get.mockReset()
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('re-fetches when the cooldown ends and stops once it has cleared', async () => {
    get.mockResolvedValueOnce(cooling(10_000)).mockResolvedValue(healthy)
    const { result } = renderHook(() => useProviderHealth('p1'))
    await advance(0)
    expect(result.current.health?.cooling_down).toBe(true)
    expect(get).toHaveBeenCalledTimes(1)

    await advance(11_499)
    expect(get).toHaveBeenCalledTimes(1)
    await advance(1)
    expect(get).toHaveBeenCalledTimes(2)
    expect(result.current.health?.cooling_down).toBe(false)

    await advance(120_000)
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('keeps re-checking every 30 s during a long cooldown, and after a failed re-check', async () => {
    get
      .mockResolvedValueOnce(cooling(10 * 60_000))
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValue(cooling(10 * 60_000))
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const { result } = renderHook(() => useProviderHealth('p1'))
    await advance(0)

    await advance(30_000)
    expect(get).toHaveBeenCalledTimes(2)
    expect(result.current.error).toBeInstanceOf(TypeError)
    expect(result.current.health?.cooling_down).toBe(true) // stale value kept

    await advance(30_000)
    expect(get).toHaveBeenCalledTimes(3)
    expect(result.current.error).toBeNull()
  })

  it('re-checks after 5 s when the cooldown is already past due (clock skew)', async () => {
    get.mockResolvedValueOnce(cooling(-60_000)).mockResolvedValue(healthy)
    renderHook(() => useProviderHealth('p1'))
    await advance(0)
    await advance(4_999)
    expect(get).toHaveBeenCalledTimes(1)
    await advance(1)
    expect(get).toHaveBeenCalledTimes(2)
  })

  it('does not start a second request while one is in flight', async () => {
    let resolveSlow: (v: unknown) => void = () => {}
    get
      .mockResolvedValueOnce(cooling(-60_000))
      .mockImplementationOnce(() => new Promise((r) => { resolveSlow = r }))
      .mockResolvedValue(healthy)
    const { result } = renderHook(() => useProviderHealth('p1'))
    await advance(0)

    // A manual retry stays pending past the scheduled re-check.
    act(() => { void result.current.refetch() })
    await advance(5_000)
    expect(get).toHaveBeenCalledTimes(2)

    await act(async () => resolveSlow(cooling(-60_000)))
    await advance(5_000)
    expect(get).toHaveBeenCalledTimes(3)
  })

  it('clears the timer on unmount', async () => {
    get.mockResolvedValue(cooling(10_000))
    const { unmount } = renderHook(() => useProviderHealth('p1'))
    await advance(0)
    unmount()
    await advance(60_000)
    expect(get).toHaveBeenCalledTimes(1)
  })

  it('drops the old timer when the provider changes', async () => {
    get.mockImplementation(async (path: string) => (path.includes('/p1/') ? cooling(10_000) : healthy))
    const { rerender, result } = renderHook(({ id }) => useProviderHealth(id), { initialProps: { id: 'p1' } })
    await advance(0)
    rerender({ id: 'p2' })
    await advance(0)
    expect(result.current.health?.cooling_down).toBe(false)

    await advance(60_000)
    expect(get.mock.calls.map(([p]) => p)).toEqual(['/v1/providers/p1/health', '/v1/providers/p2/health'])
  })
})

describe('CooldownBanner countdown', () => {
  afterEach(() => {
    vi.useRealTimers()
  })

  it('counts the minutes down without a new health response', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(NOW)
    await i18n.changeLanguage('en')
    render(<CooldownBanner health={cooling(3 * 60_000)} locale="en" />)
    expect(screen.getByText(/Should reset in about 3 min\./)).toBeInTheDocument()

    await advance(60_000)
    expect(screen.getByText(/Should reset in about 2 min\./)).toBeInTheDocument()

    await advance(2 * 60_000)
    expect(screen.queryByText(/Should reset in/)).not.toBeInTheDocument()
  })
})

describe('provider usage plural strings', () => {
  it('uses singular and plural forms in English', () => {
    const t = i18n.getFixedT('en', 'providers')
    expect(t('usage.recentErrors.count', { count: 1 })).toBe('1 call')
    expect(t('usage.recentErrors.count', { count: 3 })).toBe('3 calls')
    expect(t('usage.metrics.errorRateDetail', { count: 1, errors: '0', calls: '1' })).toBe('0 of 1 call')
    expect(t('usage.metrics.errorRateDetail', { count: 20, errors: '2', calls: '20' })).toBe('2 of 20 calls')
    expect(t('usage.chart.barLabel', { count: 1, date: 'Nov 6', tokens: '1' })).toBe('Nov 6: 1 token')
    expect(t('usage.chart.barLabel', { count: 2, date: 'Nov 6', tokens: '2' })).toBe('Nov 6: 2 tokens')
  })

  it('uses Russian one/few/many forms', () => {
    const t = i18n.getFixedT('ru', 'providers')
    expect(t('usage.recentErrors.count', { count: 1 })).toBe('1 вызов')
    expect(t('usage.recentErrors.count', { count: 3 })).toBe('3 вызова')
    expect(t('usage.recentErrors.count', { count: 5 })).toBe('5 вызовов')
    expect(t('usage.recentErrors.count', { count: 21 })).toBe('21 вызов')
  })

  it('resolves every plural key in all languages', () => {
    const keys = ['usage.recentErrors.count', 'usage.metrics.errorRateDetail', 'usage.chart.barLabel']
    for (const lng of ['en', 'ru', 'vi', 'zh']) {
      const t = i18n.getFixedT(lng, 'providers')
      for (const key of keys) {
        for (const count of [0, 1, 2, 5, 21, 1.5]) {
          const out = t(key, { count, errors: 'E', calls: 'C', date: 'D', tokens: 'T' })
          expect(out, `${lng} ${key} ${count}`).not.toContain(key)
          expect(out, `${lng} ${key} ${count}`).not.toMatch(/\{\{/)
        }
      }
    }
  })
})

describe('ProviderNameCell', () => {
  const provider: ProviderData = { id: 'p1', name: 'dashscope', display_name: 'DashScope', provider_type: 'dashscope', enabled: true }

  it('opens the detail from a dedicated name button and shows the type label', async () => {
    await i18n.changeLanguage('en')
    const onOpen = vi.fn()
    const onRowClick = vi.fn()
    const { container } = render(
      <div onClick={onRowClick}>
        <ProviderNameCell provider={provider} onOpen={onOpen} />
      </div>,
    )

    // Only the name button is interactive inside the cell.
    expect(container.querySelectorAll('button')).toHaveLength(1)
    expect(container.querySelector('[role="button"]')).toBeNull()
    expect(container.querySelector('[tabindex]')).toBeNull()
    expect(screen.getByText('DashScope (Qwen)')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'Open details for DashScope' }))
    expect(onOpen).toHaveBeenCalledTimes(1)
    expect(onOpen).toHaveBeenCalledWith(provider)
    // The click must not bubble to the row, which would open the detail twice.
    expect(onRowClick).not.toHaveBeenCalled()
  })
})

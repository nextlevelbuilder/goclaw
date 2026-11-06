import { describe, it, expect, vi, beforeEach, beforeAll } from 'vitest'
import { act, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react'
import healthFixture from './fixtures/provider-health.json'
import usageDetailFixture from './fixtures/provider-usage-detail.json'

const { get, getWithParams } = vi.hoisted(() => ({ get: vi.fn(), getWithParams: vi.fn() }))
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  getApiClient: () => ({ get, getWithParams }),
}))

import i18n from '../i18n'
import { ApiError } from '../lib/api'
import { ProviderDetail } from '../components/providers/ProviderDetail'
import { useProviderUsage } from '../hooks/use-provider-usage'
import type { ProviderData, UsageRange } from '../types/provider'

const PROVIDER: ProviderData = {
  id: '0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b',
  name: 'dashscope',
  display_name: 'DashScope',
  provider_type: 'dashscope',
  api_base: 'https://dashscope.example.com',
  enabled: true,
}

const SUMMARY = {
  current: { requests: 1284, input_tokens: 3_000_000, output_tokens: 1_100_000, cost: 1.92, unique_users: 2, errors: 80, llm_calls: 1284, tool_calls: 0, avg_duration_ms: 1500 },
  previous: {},
}

function usageDetailCalls(): number {
  return getWithParams.mock.calls.filter(([p]) => String(p).endsWith('/usage-detail')).length
}

function mockHealthy(detail: unknown = usageDetailFixture) {
  getWithParams.mockImplementation(async (path: string) => {
    if (path.endsWith('/usage-detail')) return detail
    if (path === '/v1/usage/summary') return SUMMARY
    return { points: [{ bucket_time: new Date().toISOString().slice(0, 10) + 'T00:00:00Z', input_tokens: 5, output_tokens: 5, total_cost: 0, request_count: 1, error_count: 0 }] }
  })
}

beforeAll(async () => {
  await i18n.changeLanguage('en')
})

beforeEach(() => {
  get.mockReset()
  getWithParams.mockReset()
})

describe('ProviderDetail usage tab', () => {
  it('shows the cooldown banner, metrics and translated recent errors', async () => {
    const cooldownUntil = new Date(Date.now() + 4 * 60_000 - 1000).toISOString()
    get.mockResolvedValue({ ...healthFixture, cooldown_until: cooldownUntil })
    mockHealthy()

    render(<ProviderDetail provider={PROVIDER} onBack={() => {}} onEdit={() => {}} />)
    expect(getWithParams).not.toHaveBeenCalled() // usage loads lazily on the Usage tab

    // Header status chip is a word, not only a color.
    expect(await screen.findByText('Rate limited')).toBeInTheDocument()

    fireEvent.click(screen.getByRole('tab', { name: 'Usage' }))
    expect(await screen.findByText('1,284')).toBeInTheDocument()
    expect(screen.getByText(/^Rate limited since /)).toBeInTheDocument()
    expect(screen.getByText(/Should reset in about 4 min\./)).toBeInTheDocument()
    expect(screen.getByText('4.1M')).toBeInTheDocument()
    expect(screen.getByText('$1.92 estimated')).toBeInTheDocument()
    expect(screen.getByText('6.2%')).toBeInTheDocument()
    expect(screen.getByText('1.4 s')).toBeInTheDocument()
    expect(screen.getByText('p95 4.8 s')).toBeInTheDocument()
    expect(screen.getByText('Tokens per day')).toBeInTheDocument()
    expect(screen.getByText('Authentication failed')).toBeInTheDocument()
    expect(screen.queryByText(/Above 5%/)).not.toBeInTheDocument()
  })

  it('shows no banner for a healthy provider and the empty state when there were no calls', async () => {
    get.mockResolvedValue({ ...healthFixture, cooling_down: false, cooldown_until: null, last_error_class: '' })
    getWithParams.mockImplementation(async (path: string) => {
      if (path.endsWith('/usage-detail')) return { provider: 'dashscope', calls: 0, errors: 0, latency_ms: { p50: 0, p95: 0 }, recent_errors: [] }
      if (path === '/v1/usage/summary') return { current: {}, previous: {} }
      return { points: [] }
    })

    render(<ProviderDetail provider={PROVIDER} onBack={() => {}} onEdit={() => {}} />)
    fireEvent.click(screen.getByRole('tab', { name: 'Usage' }))
    expect(await screen.findByText('No calls in this range')).toBeInTheDocument()
    expect(screen.getByText('Running')).toBeInTheDocument()
    expect(screen.queryByText(/since/)).not.toBeInTheDocument()
  })

  it('shows an error card with Try again, and recovers', async () => {
    get.mockResolvedValue(healthFixture)
    getWithParams.mockRejectedValue(new ApiError('boom', 500))

    render(<ProviderDetail provider={PROVIDER} onBack={() => {}} onEdit={() => {}} />)
    fireEvent.click(screen.getByRole('tab', { name: 'Usage' }))
    expect(await screen.findByText("Couldn't load usage")).toBeInTheDocument()

    mockHealthy()
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByText('1,284')).toBeInTheDocument()
  })

  it('explains a 503 as tracing being unavailable', async () => {
    get.mockResolvedValue(healthFixture)
    getWithParams.mockRejectedValue(new ApiError('tracing store unavailable', 503))

    render(<ProviderDetail provider={PROVIDER} onBack={() => {}} onEdit={() => {}} />)
    fireEvent.click(screen.getByRole('tab', { name: 'Usage' }))
    expect(await screen.findByText(/tracing is turned off/)).toBeInTheDocument()
  })

  it('reuses cached data when switching back to a range', async () => {
    get.mockResolvedValue(healthFixture)
    mockHealthy()

    render(<ProviderDetail provider={PROVIDER} onBack={() => {}} onEdit={() => {}} />)
    fireEvent.click(screen.getByRole('tab', { name: 'Usage' }))
    // Defaults to the 7-day range.
    expect(screen.getByRole('radio', { name: '7 days' })).toHaveAttribute('aria-checked', 'true')
    await screen.findByText('183 per day')
    expect(usageDetailCalls()).toBe(1)

    fireEvent.click(screen.getByRole('radio', { name: '24h' }))
    await waitFor(() => expect(usageDetailCalls()).toBe(2))
    await screen.findByText('1,284 per day')

    fireEvent.click(screen.getByRole('radio', { name: '7 days' }))
    expect(screen.getByText('183 per day')).toBeInTheDocument()
    expect(usageDetailCalls()).toBe(2)
  })
})

describe('useProviderUsage', () => {
  it('keeps stale data with an error when a refresh fails (offline)', async () => {
    mockHealthy()
    const { result } = renderHook(({ range }: { range: UsageRange }) => useProviderUsage('id-1', 'dashscope', range), {
      initialProps: { range: '24h' },
    })
    expect(result.current.loading).toBe(true)
    await waitFor(() => expect(result.current.data).not.toBeNull())
    expect(result.current.data?.detail.calls).toBe(1284)

    getWithParams.mockRejectedValue(new TypeError('Failed to fetch'))
    act(() => result.current.refetch())
    await waitFor(() => expect(result.current.staleError).toBeInstanceOf(TypeError))
    expect(result.current.data?.detail.calls).toBe(1284)
    expect(result.current.error).toBeNull()
    expect(result.current.fetchedAt).not.toBeNull()
  })

  it('does not fetch while disabled', () => {
    renderHook(() => useProviderUsage('id-1', 'dashscope', '24h', false))
    expect(getWithParams).not.toHaveBeenCalled()
  })
})

// Provider health + usage service — wraps HTTP /v1/providers/{id}/health,
// /v1/providers/{id}/usage-detail and the provider-filtered /v1/usage/* endpoints.
// Every response is validated here so components only ever see well-formed data.
import { getApiClient } from '../lib/api'
import type {
  ProviderHealth,
  ProviderUsageBundle,
  ProviderUsageDetail,
  ProviderUsageErrorItem,
  UsageRange,
  UsageSummary,
  UsageSummaryPeriod,
  UsageTimeSeriesPoint,
} from '../types/provider'

const RANGE_MS: Record<UsageRange, number> = {
  '24h': 24 * 60 * 60 * 1000,
  '7d': 7 * 24 * 60 * 60 * 1000,
  '30d': 30 * 24 * 60 * 60 * 1000,
}

/** Number of days a range covers; used for "per day" averages. */
export const RANGE_DAYS: Record<UsageRange, number> = { '24h': 1, '7d': 7, '30d': 30 }

/** Maximum recent-error groups requested from usage-detail. */
const RECENT_ERRORS_LIMIT = 20

type Obj = Record<string, unknown>

function asObject(raw: unknown, what: string): Obj {
  if (raw === null || typeof raw !== 'object' || Array.isArray(raw)) {
    throw new Error(`${what} must be an object`)
  }
  return raw as Obj
}

function num(o: Obj, key: string, what: string, fallback?: number): number {
  const v = o[key]
  if (typeof v === 'number' && Number.isFinite(v)) return v
  if (v === undefined || v === null) {
    if (fallback !== undefined) return fallback
  }
  throw new Error(`${what}.${key} must be a number`)
}

function str(o: Obj, key: string, what: string, fallback?: string): string {
  const v = o[key]
  if (typeof v === 'string') return v
  if ((v === undefined || v === null) && fallback !== undefined) return fallback
  throw new Error(`${what}.${key} must be a string`)
}

function nullableTime(o: Obj, key: string, what: string): string | null {
  const v = o[key]
  if (v === undefined || v === null) return null
  if (typeof v === 'string' && !Number.isNaN(Date.parse(v))) return v
  throw new Error(`${what}.${key} must be an RFC3339 time or null`)
}

/** Validates GET /v1/providers/{id}/health. */
export function parseProviderHealth(raw: unknown): ProviderHealth {
  const what = 'provider health'
  const o = asObject(raw, what)
  if (typeof o.cooling_down !== 'boolean') throw new Error(`${what}.cooling_down must be a boolean`)
  const counts: Record<string, number> = {}
  if (o.error_counts !== undefined && o.error_counts !== null) {
    for (const [k, v] of Object.entries(asObject(o.error_counts, `${what}.error_counts`))) {
      if (typeof v === 'number' && Number.isFinite(v)) counts[k] = v
    }
  }
  return {
    provider_id: str(o, 'provider_id', what),
    provider: str(o, 'provider', what),
    enabled: o.enabled !== false,
    consecutive_failures: num(o, 'consecutive_failures', what, 0),
    cooling_down: o.cooling_down,
    cooldown_until: nullableTime(o, 'cooldown_until', what),
    last_error_class: str(o, 'last_error_class', what, ''),
    error_counts: counts,
    last_probe_at: nullableTime(o, 'last_probe_at', what),
    updated_at: nullableTime(o, 'updated_at', what),
    max_cooldown_seconds: num(o, 'max_cooldown_seconds', what, 0),
  }
}

/** Validates GET /v1/providers/{id}/usage-detail. Malformed error items are dropped. */
export function parseProviderUsageDetail(raw: unknown): ProviderUsageDetail {
  const what = 'provider usage detail'
  const o = asObject(raw, what)
  const latency = asObject(o.latency_ms, `${what}.latency_ms`)
  const rawErrors = o.recent_errors ?? []
  if (!Array.isArray(rawErrors)) throw new Error(`${what}.recent_errors must be an array`)
  const recent: ProviderUsageErrorItem[] = []
  for (const item of rawErrors) {
    if (!item || typeof item !== 'object') continue
    const e = item as Obj
    if (typeof e.at !== 'string' || Number.isNaN(Date.parse(e.at))) continue
    if (typeof e.count !== 'number' || !Number.isFinite(e.count)) continue
    recent.push({ at: e.at, error_class: typeof e.error_class === 'string' ? e.error_class : 'unknown', count: e.count })
  }
  return {
    provider: str(o, 'provider', what),
    calls: num(o, 'calls', what),
    errors: num(o, 'errors', what),
    latency_ms: { p50: num(latency, 'p50', `${what}.latency_ms`, 0), p95: num(latency, 'p95', `${what}.latency_ms`, 0) },
    recent_errors: recent,
  }
}

function parseSummaryPeriod(raw: unknown, what: string): UsageSummaryPeriod {
  const o = asObject(raw, what)
  return {
    requests: num(o, 'requests', what, 0),
    input_tokens: num(o, 'input_tokens', what, 0),
    output_tokens: num(o, 'output_tokens', what, 0),
    cost: num(o, 'cost', what, 0),
    unique_users: num(o, 'unique_users', what, 0),
    errors: num(o, 'errors', what, 0),
    llm_calls: num(o, 'llm_calls', what, 0),
    tool_calls: num(o, 'tool_calls', what, 0),
    avg_duration_ms: num(o, 'avg_duration_ms', what, 0),
  }
}

/** Validates GET /v1/usage/summary. */
export function parseUsageSummary(raw: unknown): UsageSummary {
  const o = asObject(raw, 'usage summary')
  return {
    current: parseSummaryPeriod(o.current, 'usage summary.current'),
    previous: parseSummaryPeriod(o.previous ?? {}, 'usage summary.previous'),
  }
}

/** Validates GET /v1/usage/timeseries. Points without a valid bucket_time are dropped. */
export function parseUsageTimeSeries(raw: unknown): UsageTimeSeriesPoint[] {
  const o = asObject(raw, 'usage timeseries')
  const points = o.points ?? []
  if (!Array.isArray(points)) throw new Error('usage timeseries.points must be an array')
  const out: UsageTimeSeriesPoint[] = []
  for (const item of points) {
    if (!item || typeof item !== 'object') continue
    const p = item as Obj
    if (typeof p.bucket_time !== 'string' || Number.isNaN(Date.parse(p.bucket_time))) continue
    const what = 'usage timeseries point'
    out.push({
      bucket_time: p.bucket_time,
      input_tokens: num(p, 'input_tokens', what, 0),
      output_tokens: num(p, 'output_tokens', what, 0),
      total_cost: num(p, 'total_cost', what, 0),
      request_count: num(p, 'request_count', what, 0),
      error_count: num(p, 'error_count', what, 0),
    })
  }
  return out
}

/** RFC3339 [from, to) window for a range ending at `now`. */
export function rangeWindow(range: UsageRange, now: Date = new Date()): { from: string; to: string } {
  const to = new Date(Math.floor(now.getTime() / 1000) * 1000)
  const from = new Date(to.getTime() - RANGE_MS[range])
  return { from: toRFC3339(from), to: toRFC3339(to) }
}

/** Second-precision RFC3339 in UTC, matching the gateway's time.RFC3339 parser. */
function toRFC3339(d: Date): string {
  return d.toISOString().replace(/\.\d{3}Z$/, 'Z')
}

export interface UsageOverview {
  summary: UsageSummary
  points: UsageTimeSeriesPoint[]
}

export const usageService = {
  async health(providerId: string): Promise<ProviderHealth> {
    const raw = await getApiClient().get<unknown>(`/v1/providers/${encodeURIComponent(providerId)}/health`)
    return parseProviderHealth(raw)
  },

  async usageDetail(providerId: string, from: string, to: string, limit = RECENT_ERRORS_LIMIT): Promise<ProviderUsageDetail> {
    const raw = await getApiClient().getWithParams<unknown>(
      `/v1/providers/${encodeURIComponent(providerId)}/usage-detail`,
      { from, to, limit: String(limit) },
    )
    return parseProviderUsageDetail(raw)
  },

  /** `providerName` is the provider name (e.g. "dashscope"), not its UUID. */
  async summary(providerName: string, range: UsageRange): Promise<UsageSummary> {
    const raw = await getApiClient().getWithParams<unknown>('/v1/usage/summary', { period: range, provider: providerName })
    return parseUsageSummary(raw)
  },

  async dailyTimeSeries(providerName: string, from: string, to: string): Promise<UsageTimeSeriesPoint[]> {
    const raw = await getApiClient().getWithParams<unknown>('/v1/usage/timeseries', {
      from,
      to,
      group_by: 'day',
      provider: providerName,
    })
    return parseUsageTimeSeries(raw)
  },

  /** Totals across all providers for the Usage view (no provider filter). */
  async overview(range: UsageRange, now: Date = new Date()): Promise<UsageOverview> {
    const { from, to } = rangeWindow(range, now)
    const api = getApiClient()
    const [summaryRaw, seriesRaw] = await Promise.all([
      api.getWithParams<unknown>('/v1/usage/summary', { period: range }),
      api.getWithParams<unknown>('/v1/usage/timeseries', { from, to, group_by: 'day' }),
    ])
    return { summary: parseUsageSummary(summaryRaw), points: parseUsageTimeSeries(seriesRaw) }
  },

  /** Fetches everything the Usage tab needs for one provider and range, in parallel. */
  async bundle(providerId: string, providerName: string, range: UsageRange, now: Date = new Date()): Promise<ProviderUsageBundle> {
    const { from, to } = rangeWindow(range, now)
    const [detail, summary, points] = await Promise.all([
      usageService.usageDetail(providerId, from, to),
      usageService.summary(providerName, range),
      usageService.dailyTimeSeries(providerName, from, to),
    ])
    return { detail, summary: summary.current, points }
  },
}

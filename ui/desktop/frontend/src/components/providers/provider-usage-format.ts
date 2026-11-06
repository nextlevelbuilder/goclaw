// Pure helpers for the provider Usage tab: derived metrics, status and formatting.
import { PROVIDER_ERROR_CLASSES } from '../../types/provider'
import type { ProviderHealth, UsageRange, UsageTimeSeriesPoint } from '../../types/provider'
import { RANGE_DAYS } from '../../services/usage-service'

const KNOWN_ERROR_CLASSES = new Set<string>(PROVIDER_ERROR_CLASSES)
const DAY_MS = 24 * 60 * 60 * 1000

/** i18n key (providers namespace) for an error class; unrecognised classes use `unknown`. */
export function errorClassKey(errorClass: string | null | undefined): string {
  const cls = errorClass && KNOWN_ERROR_CLASSES.has(errorClass) ? errorClass : 'unknown'
  return `usage.errorClass.${cls}`
}

/** Failed share of calls in [0, 1], or null when there were no calls. */
export function errorRate(calls: number, errors: number): number | null {
  if (!Number.isFinite(calls) || calls <= 0) return null
  const e = Math.max(0, Math.min(errors, calls))
  return e / calls
}

/** Average requests per day for a range, rounded to a whole number. */
export function requestsPerDay(requests: number, range: UsageRange): number {
  return Math.round(requests / RANGE_DAYS[range])
}

export interface LatencyParts {
  value: string
  unit: 'seconds' | 'milliseconds'
}

/** Latency for display: whole milliseconds below 1 s, otherwise seconds with one decimal. */
export function formatLatency(ms: number, locale?: string): LatencyParts {
  const safe = Number.isFinite(ms) && ms > 0 ? ms : 0
  if (safe < 1000) {
    return { value: new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(safe), unit: 'milliseconds' }
  }
  return {
    value: new Intl.NumberFormat(locale, { minimumFractionDigits: 1, maximumFractionDigits: 1 }).format(safe / 1000),
    unit: 'seconds',
  }
}

export function formatCompactNumber(n: number, locale?: string): string {
  return new Intl.NumberFormat(locale, { notation: 'compact', maximumFractionDigits: 1 }).format(n)
}

export function formatInteger(n: number, locale?: string): string {
  return new Intl.NumberFormat(locale, { maximumFractionDigits: 0 }).format(n)
}

export function formatPercent(rate: number, locale?: string): string {
  return new Intl.NumberFormat(locale, { style: 'percent', maximumFractionDigits: 1 }).format(rate)
}

/** Gateway costs are USD estimates. */
export function formatCost(usd: number, locale?: string): string {
  return new Intl.NumberFormat(locale, {
    style: 'currency',
    currency: 'USD',
    minimumFractionDigits: 2,
    maximumFractionDigits: usd > 0 && usd < 0.01 ? 4 : 2,
  }).format(usd)
}

/** Local HH:MM. */
export function formatClock(iso: string, locale?: string): string {
  return new Intl.DateTimeFormat(locale, { hour: '2-digit', minute: '2-digit' }).format(new Date(iso))
}

/** Local HH:MM for today, otherwise a short date plus HH:MM. */
export function formatErrorTime(iso: string, locale?: string, now: Date = new Date()): string {
  const d = new Date(iso)
  if (d.toDateString() === now.toDateString()) return formatClock(iso, locale)
  return new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(d)
}

export type ProviderStatus = 'running' | 'rateLimited' | 'coolingDown' | 'disabled'

/** Status word for the detail header chip. Health is optional (it may still be loading or unavailable). */
export function providerStatus(enabled: boolean, health: ProviderHealth | null | undefined): ProviderStatus {
  if (!enabled || health?.enabled === false) return 'disabled'
  if (health?.cooling_down) return health.last_error_class === 'rate_limit' ? 'rateLimited' : 'coolingDown'
  return 'running'
}

/** Whole minutes (at least 1) until the cooldown ends, or null when unknown or already over. */
export function cooldownMinutesLeft(cooldownUntil: string | null, now: Date = new Date()): number | null {
  if (!cooldownUntil) return null
  const ms = Date.parse(cooldownUntil) - now.getTime()
  if (!Number.isFinite(ms) || ms <= 0) return null
  return Math.max(1, Math.ceil(ms / 60_000))
}

export interface DailyTokens {
  /** UTC day, `YYYY-MM-DD`. */
  day: string
  tokens: number
}

/**
 * One bar per UTC day covered by the range ending at `now` (gateway day buckets are
 * UTC midnights). Days without a point are zero so the chart keeps its time axis.
 */
export function dailyTokenBars(points: UsageTimeSeriesPoint[], range: UsageRange, now: Date = new Date()): DailyTokens[] {
  const byDay = new Map<string, number>()
  for (const p of points) {
    const day = new Date(p.bucket_time).toISOString().slice(0, 10)
    byDay.set(day, (byDay.get(day) ?? 0) + p.input_tokens + p.output_tokens)
  }
  const utcMidnight = (d: Date) => Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate())
  const end = utcMidnight(now)
  const start = utcMidnight(new Date(now.getTime() - RANGE_DAYS[range] * DAY_MS))
  const out: DailyTokens[] = []
  for (let t = start; t <= end; t += DAY_MS) {
    const day = new Date(t).toISOString().slice(0, 10)
    out.push({ day, tokens: byDay.get(day) ?? 0 })
  }
  return out
}

/** Short axis label for a UTC day: weekday for up to a week, otherwise day of month. */
export function formatDayLabel(day: string, range: UsageRange, locale?: string): string {
  const opts: Intl.DateTimeFormatOptions = range === '30d' ? { day: 'numeric' } : { weekday: 'short' }
  return new Intl.DateTimeFormat(locale, { ...opts, timeZone: 'UTC' }).format(new Date(`${day}T00:00:00Z`))
}

/** Full label for a UTC day, used in bar tooltips and accessible names. */
export function formatDayLong(day: string, locale?: string): string {
  return new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', timeZone: 'UTC' }).format(new Date(`${day}T00:00:00Z`))
}

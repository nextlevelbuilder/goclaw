// Presentational pieces of the provider Usage tab. Status is always icon + word, never color alone.
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { IconAlertTriangle } from '../common/Icons'
import { StatusChip, type StatusTone } from '../ui'
import type { ProviderHealth, ProviderUsageErrorItem, UsageRange, UsageTimeSeriesPoint } from '../../types/provider'
import {
  cooldownMinutesLeft,
  dailyTokenBars,
  errorClassKey,
  formatClock,
  formatCompactNumber,
  formatDayLabel,
  formatDayLong,
  formatErrorTime,
  formatInteger,
  type ProviderStatus,
} from './provider-usage-format'

const TONE: Record<ProviderStatus, StatusTone> = {
  running: 'success',
  rateLimited: 'warning',
  coolingDown: 'warning',
  disabled: 'idle',
}

/** Provider state as icon + word (never colour alone). */
export function ProviderStatusChip({ status }: { status: ProviderStatus }) {
  const { t } = useTranslation('providers')
  return <StatusChip tone={TONE[status]} label={t(`providerDetail.status.${status}`)} />
}

/** Re-render interval for the "resets in N min" countdown. */
const COUNTDOWN_TICK_MS = 20_000

/** Current time, refreshed every COUNTDOWN_TICK_MS while `active`. */
function useNow(active: boolean): Date {
  const [now, setNow] = useState(() => new Date())
  useEffect(() => {
    if (!active) return
    setNow(new Date())
    const timer = setInterval(() => setNow(new Date()), COUNTDOWN_TICK_MS)
    return () => clearInterval(timer)
  }, [active])
  return now
}

/** Shown only while the provider is cooling down. */
export function CooldownBanner({ health, locale }: { health: ProviderHealth; locale: string }) {
  const { t } = useTranslation('providers')
  const now = useNow(health.cooling_down)
  if (!health.cooling_down) return null
  const rateLimited = health.last_error_class === 'rate_limit'
  const since = health.updated_at ? formatClock(health.updated_at, locale) : null
  const title = since
    ? t(rateLimited ? 'usage.banner.rateLimitedSince' : 'usage.banner.coolingDownSince', { time: since })
    : t(rateLimited ? 'usage.banner.rateLimited' : 'usage.banner.coolingDown')
  const minutes = cooldownMinutesLeft(health.cooldown_until, now)
  const details = [
    !rateLimited && health.last_error_class
      ? t('usage.banner.reason', { reason: t(errorClassKey(health.last_error_class)) })
      : null,
    minutes !== null ? t('usage.banner.resetIn', { minutes }) : null,
    health.consecutive_failures > 0 ? t('usage.banner.failures', { count: health.consecutive_failures }) : null,
  ].filter(Boolean)

  return (
    <div role="status" className="flex items-start gap-2 rounded-lg bg-warning-subtle px-3 py-2.5 text-warning">
      <IconAlertTriangle size={16} className="mt-0.5 shrink-0" />
      <div className="min-w-0">
        <p className="text-caption font-medium">{title}</p>
        {details.length > 0 && <p className="text-caption mt-0.5">{details.join(' ')}</p>}
      </div>
    </div>
  )
}

export function MetricCard({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="rounded-lg border border-border-subtle bg-surface-raised px-3 py-2.5 min-w-0">
      <p className="text-caption text-ink-muted">{label}</p>
      <p className="text-title text-ink tabular-nums truncate">{value}</p>
      {sub && <p className="text-caption text-ink-muted truncate">{sub}</p>}
    </div>
  )
}

export function TokensPerDayChart({ points, range, locale }: { points: UsageTimeSeriesPoint[]; range: UsageRange; locale: string }) {
  const { t } = useTranslation('providers')
  const bars = dailyTokenBars(points, range)
  const max = Math.max(0, ...bars.map((b) => b.tokens))

  return (
    <section className="rounded-lg border border-border-subtle bg-surface-raised px-3 py-2.5 space-y-2">
      <h4 className="text-ui text-ink">{t('usage.chart.title')}</h4>
      {max === 0 ? (
        <p className="text-caption text-ink-muted py-6 text-center">{t('usage.chart.empty')}</p>
      ) : (
        <ul className="flex items-end gap-1 h-28" aria-label={t('usage.chart.title')}>
          {bars.map((b, i) => {
            const label = t('usage.chart.barLabel', {
              count: b.tokens,
              date: formatDayLong(b.day, locale),
              tokens: formatInteger(b.tokens, locale),
            })
            return (
              <li key={b.day} className="flex-1 min-w-0 h-full flex flex-col items-center justify-end gap-1" title={label}>
                <span className="sr-only">{label}</span>
                <div
                  className="w-full max-w-8 rounded-t-sm bg-accent"
                  style={{ height: `${b.tokens === 0 ? 0 : Math.max(2, (b.tokens / max) * 100)}%` }}
                />
                {/* 30-day ranges label every fifth bar to stay readable. */}
                <span aria-hidden className="text-caption text-ink-muted truncate h-4">
                  {range !== '30d' || i % 5 === 0 ? formatDayLabel(b.day, range, locale) : ''}
                </span>
              </li>
            )
          })}
        </ul>
      )}
      {max > 0 && (
        <p className="text-caption text-ink-muted text-right tabular-nums" aria-hidden>
          {formatCompactNumber(max, locale)}
        </p>
      )}
    </section>
  )
}

export function RecentErrorsList({ items, locale }: { items: ProviderUsageErrorItem[]; locale: string }) {
  const { t } = useTranslation('providers')
  return (
    <section className="rounded-lg border border-border-subtle bg-surface-raised px-3 py-2.5 space-y-2">
      <h4 className="text-ui text-ink">{t('usage.recentErrors.title')}</h4>
      {items.length === 0 ? (
        <p className="text-caption text-ink-muted py-2">{t('usage.recentErrors.empty')}</p>
      ) : (
        <ul className="divide-y divide-border-subtle">
          {items.map((e) => (
            <li key={`${e.at}-${e.error_class}`} className="flex items-center gap-3 py-1.5 text-caption">
              <span className="text-ink-muted tabular-nums shrink-0 w-24">{formatErrorTime(e.at, locale)}</span>
              <span className="inline-flex items-center gap-1 text-ink flex-1 min-w-0 truncate">
                <IconAlertTriangle size={12} className="shrink-0 text-warning" />
                {t(errorClassKey(e.error_class))}
              </span>
              <span
                className="text-caption px-1.5 py-0.5 rounded-pill bg-surface-hover text-ink-secondary tabular-nums shrink-0"
                title={t('usage.recentErrors.count', { count: e.count })}
              >
                <span aria-hidden>×{formatInteger(e.count, locale)}</span>
                <span className="sr-only">{t('usage.recentErrors.count', { count: e.count })}</span>
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  )
}

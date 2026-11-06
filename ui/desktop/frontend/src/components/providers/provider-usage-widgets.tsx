// Presentational pieces of the provider Usage tab. Status is always icon + word, never color alone.
import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
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

const iconProps = {
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 2,
  strokeLinecap: 'round' as const,
  strokeLinejoin: 'round' as const,
  'aria-hidden': true,
}

export function WarningIcon({ className = 'w-3 h-3' }: { className?: string }) {
  return (
    <svg className={className} {...iconProps}>
      <path d="M10.29 3.86 1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
      <line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" />
    </svg>
  )
}

function CheckIcon() {
  return <svg className="w-3 h-3" {...iconProps}><polyline points="20 6 9 17 4 12" /></svg>
}

function BlockedIcon() {
  return <svg className="w-3 h-3" {...iconProps}><circle cx="12" cy="12" r="10" /><line x1="4.93" y1="4.93" x2="19.07" y2="19.07" /></svg>
}

const STATUS_STYLE: Record<ProviderStatus, string> = {
  running: 'bg-success-subtle text-success-text',
  rateLimited: 'bg-warning-subtle text-warning-text',
  coolingDown: 'bg-warning-subtle text-warning-text',
  disabled: 'bg-surface-tertiary text-text-muted',
}

export function ProviderStatusChip({ status }: { status: ProviderStatus }) {
  const { t } = useTranslation('providers')
  return (
    <span className={`inline-flex items-center gap-1 text-2xs px-1.5 py-0.5 rounded-full shrink-0 ${STATUS_STYLE[status]}`}>
      {status === 'running' ? <CheckIcon /> : status === 'disabled' ? <BlockedIcon /> : <WarningIcon />}
      {t(`providerDetail.status.${status}`)}
    </span>
  )
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
    <div role="status" className="flex items-start gap-2 rounded-lg border border-warning/40 bg-warning-subtle px-3 py-2.5 text-warning-text">
      <WarningIcon className="w-4 h-4 mt-0.5 shrink-0" />
      <div className="min-w-0">
        <p className="text-xs font-medium">{title}</p>
        {details.length > 0 && <p className="text-2xs mt-0.5">{details.join(' ')}</p>}
      </div>
    </div>
  )
}

export function MetricCard({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="rounded-lg border border-border bg-surface-primary/40 px-3 py-2.5 min-w-0">
      <p className="text-2xs text-text-muted">{label}</p>
      <p className="text-lg font-semibold text-text-primary tabular-nums truncate">{value}</p>
      {sub && <p className="text-2xs text-text-muted truncate">{sub}</p>}
    </div>
  )
}

export function TokensPerDayChart({ points, range, locale }: { points: UsageTimeSeriesPoint[]; range: UsageRange; locale: string }) {
  const { t } = useTranslation('providers')
  const bars = dailyTokenBars(points, range)
  const max = Math.max(0, ...bars.map((b) => b.tokens))

  return (
    <section className="rounded-lg border border-border px-3 py-2.5 space-y-2">
      <h4 className="text-xs font-medium text-text-primary">{t('usage.chart.title')}</h4>
      {max === 0 ? (
        <p className="text-2xs text-text-muted py-6 text-center">{t('usage.chart.empty')}</p>
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
                  className="w-full max-w-8 rounded-t bg-accent/70"
                  style={{ height: `${b.tokens === 0 ? 0 : Math.max(2, (b.tokens / max) * 100)}%` }}
                />
                {/* 30-day ranges label every fifth bar to stay readable. */}
                <span aria-hidden className="text-2xs text-text-muted truncate h-4">
                  {range !== '30d' || i % 5 === 0 ? formatDayLabel(b.day, range, locale) : ''}
                </span>
              </li>
            )
          })}
        </ul>
      )}
      {max > 0 && (
        <p className="text-2xs text-text-muted text-right tabular-nums" aria-hidden>
          {formatCompactNumber(max, locale)}
        </p>
      )}
    </section>
  )
}

export function RecentErrorsList({ items, locale }: { items: ProviderUsageErrorItem[]; locale: string }) {
  const { t } = useTranslation('providers')
  return (
    <section className="rounded-lg border border-border px-3 py-2.5 space-y-2">
      <h4 className="text-xs font-medium text-text-primary">{t('usage.recentErrors.title')}</h4>
      {items.length === 0 ? (
        <p className="text-2xs text-text-muted py-2">{t('usage.recentErrors.empty')}</p>
      ) : (
        <ul className="divide-y divide-border">
          {items.map((e) => (
            <li key={`${e.at}-${e.error_class}`} className="flex items-center gap-3 py-1.5 text-xs">
              <span className="text-text-muted tabular-nums shrink-0 w-24">{formatErrorTime(e.at, locale)}</span>
              <span className="inline-flex items-center gap-1 text-text-primary flex-1 min-w-0 truncate">
                <WarningIcon className="w-3 h-3 text-warning-text shrink-0" />
                {t(errorClassKey(e.error_class))}
              </span>
              <span
                className="text-2xs px-1.5 py-0.5 rounded-full bg-surface-tertiary text-text-secondary tabular-nums shrink-0"
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

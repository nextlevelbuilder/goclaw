import { useTranslation } from 'react-i18next'
import { ApiError } from '../../lib/api'
import type { ProviderHealth, UsageRange } from '../../types/provider'
import type { ProviderUsageState } from '../../hooks/use-provider-usage'
import {
  errorRate,
  formatClock,
  formatCompactNumber,
  formatCost,
  formatInteger,
  formatLatency,
  formatPercent,
  requestsPerDay,
} from './provider-usage-format'
import { CooldownBanner, MetricCard, RecentErrorsList, TokensPerDayChart, WarningIcon } from './provider-usage-widgets'

interface ProviderUsageTabProps {
  range: UsageRange
  usage: ProviderUsageState
  health: ProviderHealth | null
  /** Retries both usage and health. */
  onRetry: () => void
}

export function ProviderUsageTab({ range, usage, health, onRetry }: ProviderUsageTabProps) {
  const { t, i18n } = useTranslation('providers')
  const locale = i18n.language
  const { data } = usage

  const latency = (ms: number) => {
    const { value, unit } = formatLatency(ms, locale)
    return t(`usage.units.${unit}`, { value })
  }

  let body: React.ReactNode
  if (usage.loading) {
    body = <UsageSkeleton label={t('usage.states.loading')} />
  } else if (!data) {
    const tracingOff = usage.error instanceof ApiError && usage.error.status === 503
    body = (
      <div role="alert" className="rounded-lg border border-error/40 bg-error-subtle px-4 py-4 space-y-2">
        <p className="flex items-center gap-1.5 text-sm font-medium text-error-text">
          <WarningIcon className="w-4 h-4" />
          {t('usage.states.errorTitle')}
        </p>
        <p className="text-xs text-text-secondary">
          {t(tracingOff ? 'usage.states.tracingUnavailable' : 'usage.states.errorBody')}
        </p>
        <RetryButton onClick={onRetry} label={t('usage.states.retry')} />
      </div>
    )
  } else if (data.detail.calls === 0 && data.summary.requests === 0) {
    body = (
      <div className="rounded-lg border border-dashed border-border px-4 py-10 text-center space-y-1">
        <p className="text-sm font-medium text-text-primary">{t('usage.states.emptyTitle')}</p>
        <p className="text-xs text-text-muted">{t('usage.states.emptyBody')}</p>
      </div>
    )
  } else {
    const { detail, summary, points } = data
    const rate = errorRate(detail.calls, detail.errors)
    const na = t('usage.metrics.notAvailable')
    body = (
      <div className="space-y-3">
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-2">
          <MetricCard
            label={t('usage.metrics.tokens')}
            value={formatCompactNumber(summary.input_tokens + summary.output_tokens, locale)}
            sub={t('usage.metrics.estimatedCost', { cost: formatCost(summary.cost, locale) })}
          />
          <MetricCard
            label={t('usage.metrics.requests')}
            value={formatInteger(summary.requests, locale)}
            sub={t('usage.metrics.perDay', { value: formatInteger(requestsPerDay(summary.requests, range), locale) })}
          />
          <MetricCard
            label={t('usage.metrics.errorRate')}
            value={rate === null ? na : formatPercent(rate, locale)}
            sub={detail.calls > 0
              ? t('usage.metrics.errorRateDetail', { errors: formatInteger(detail.errors, locale), calls: formatInteger(detail.calls, locale) })
              : undefined}
          />
          <MetricCard
            label={t('usage.metrics.medianLatency')}
            value={detail.calls > 0 ? latency(detail.latency_ms.p50) : na}
            sub={detail.calls > 0 ? t('usage.metrics.p95', { value: latency(detail.latency_ms.p95) }) : undefined}
          />
        </div>
        <TokensPerDayChart points={points} range={range} locale={locale} />
        <RecentErrorsList items={detail.recent_errors} locale={locale} />
      </div>
    )
  }

  return (
    <div className="space-y-3">
      {health && <CooldownBanner health={health} locale={locale} />}
      {data && !!usage.staleError && usage.fetchedAt !== null && (
        <div role="status" className="flex items-center justify-between gap-2 rounded-lg border border-border bg-surface-tertiary/60 px-3 py-2">
          <p className="flex items-center gap-1.5 text-2xs text-text-secondary">
            <WarningIcon className="w-3 h-3 shrink-0" />
            {t('usage.states.staleNote', { time: formatClock(new Date(usage.fetchedAt).toISOString(), locale) })}
          </p>
          <RetryButton onClick={onRetry} label={t('usage.states.retry')} />
        </div>
      )}
      {body}
    </div>
  )
}

function RetryButton({ onClick, label }: { onClick: () => void; label: string }) {
  return (
    <button
      onClick={onClick}
      className="px-3 py-1 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors shrink-0"
    >
      {label}
    </button>
  )
}

/** Pulse is opt-in via motion-safe, so it stays still under prefers-reduced-motion. */
function UsageSkeleton({ label }: { label: string }) {
  const block = 'rounded-lg bg-surface-tertiary/60 motion-safe:animate-pulse'
  return (
    <div role="status" aria-busy="true" className="space-y-3">
      <span className="sr-only">{label}</span>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-2">
        {[0, 1, 2, 3].map((i) => <div key={i} className={`${block} h-[74px]`} />)}
      </div>
      <div className={`${block} h-40`} />
      <div className={`${block} h-24`} />
    </div>
  )
}

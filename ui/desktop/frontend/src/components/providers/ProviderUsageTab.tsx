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
import { IconAlertTriangle } from '../common/Icons'
import { Button, EmptyState, ErrorState, Skeleton } from '../ui'
import { CooldownBanner, MetricCard, RecentErrorsList, TokensPerDayChart } from './provider-usage-widgets'

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
      <ErrorState
        title={t('usage.states.errorTitle')}
        description={t(tracingOff ? 'usage.states.tracingUnavailable' : 'usage.states.errorBody')}
        code={usage.error instanceof ApiError ? String(usage.error.status) : undefined}
        onRetry={onRetry}
      />
    )
  } else if (data.detail.calls === 0 && data.summary.requests === 0) {
    body = <EmptyState title={t('usage.states.emptyTitle')} description={t('usage.states.emptyBody')} />
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
              ? t('usage.metrics.errorRateDetail', {
                  count: detail.calls,
                  errors: formatInteger(detail.errors, locale),
                  calls: formatInteger(detail.calls, locale),
                })
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
        <div role="status" className="flex items-center justify-between gap-2 rounded-lg border border-border-subtle bg-surface-raised px-3 py-2">
          <p className="flex items-center gap-1.5 text-caption text-ink-secondary">
            <IconAlertTriangle size={12} className="shrink-0" />
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
  return <Button size="sm" variant="secondary" onClick={onClick} className="shrink-0">{label}</Button>
}

function UsageSkeleton({ label }: { label: string }) {
  return (
    <div role="status" aria-busy="true" className="space-y-3">
      <span className="sr-only">{label}</span>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-4">
        {[0, 1, 2, 3].map((i) => <Skeleton key={i} className="h-[74px] rounded-lg" />)}
      </div>
      <Skeleton className="h-40 rounded-lg" />
      <Skeleton className="h-24 rounded-lg" />
    </div>
  )
}

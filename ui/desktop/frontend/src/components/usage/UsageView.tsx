import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import { useUsageOverview } from '../../hooks/use-usage-overview'
import { useGatewayStatus } from '../layout/use-gateway-status'
import { useUiStore } from '../../stores/ui-store'
import { ApiError } from '../../lib/api'
import { cn } from '../../lib/cn'
import { IconBarChart } from '../common/Icons'
import { Button, EmptyState, ErrorState, PageHeader, SegmentedControl, Skeleton } from '../ui'
import { formatCompactNumber, formatCost, formatInteger, formatLatency, formatPercent, errorRate } from '../providers/provider-usage-format'
import { UsageTokenChart } from './usage-token-chart'
import type { UsageRange, UsageSummaryPeriod } from '../../types/provider'

const RANGES: UsageRange[] = ['24h', '7d', '30d']

function delta(current: number, previous: number): number | null {
  if (!Number.isFinite(previous) || previous <= 0) return null
  return (current - previous) / previous
}

function Kpi({ label, value, change, previousLabel }: { label: string; value: string; change?: number | null; previousLabel: (pct: string) => string }) {
  const { i18n } = useTranslation()
  return (
    <div className="min-w-0 rounded-lg border border-border-subtle bg-surface-raised px-4 py-3">
      <p className="text-caption text-ink-muted">{label}</p>
      <p className="truncate text-title-lg tabular-nums text-ink">{value}</p>
      {change != null && (
        <p className="truncate text-caption tabular-nums text-ink-muted">
          {previousLabel(`${change > 0 ? '+' : ''}${formatPercent(change, i18n.language)}`)}
        </p>
      )}
    </div>
  )
}

function isEmpty(s: UsageSummaryPeriod): boolean {
  return s.requests === 0 && s.input_tokens === 0 && s.output_tokens === 0 && s.llm_calls === 0 && s.tool_calls === 0
}

export function UsageView() {
  const { t, i18n } = useTranslation(['usage', 'common'])
  const [range, setRange] = useState<UsageRange>('7d')
  const { data, loading, error, reload } = useUsageOverview(range)
  const { disconnected } = useGatewayStatus()
  const setActiveView = useUiStore((s) => s.setActiveView)
  const locale = i18n.language

  const rangeControl = (
    <SegmentedControl
      aria-label={t('usage:range.label')}
      value={range}
      onValueChange={(v) => setRange(v as UsageRange)}
      options={RANGES.map((r) => ({ value: r, label: t(`usage:range.${r}`) }))}
    />
  )

  let body: React.ReactNode
  if (loading && !data) {
    body = (
      <div role="status" aria-label={t('common:state.loading')} className="space-y-4">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {[0, 1, 2, 3].map((i) => <Skeleton key={i} className="h-20 rounded-lg" />)}
        </div>
        <Skeleton className="h-60 rounded-lg" />
      </div>
    )
  } else if (error) {
    body = (
      <ErrorState
        title={t('usage:states.errorTitle')}
        description={t('usage:states.errorBody')}
        code={error instanceof ApiError ? String(error.status) : undefined}
        onRetry={() => { void reload() }}
        retrying={loading}
      />
    )
  } else if (!data || isEmpty(data.summary.current)) {
    body = (
      <EmptyState
        icon={<IconBarChart size={20} />}
        title={t('usage:emptyTitle')}
        description={t('usage:emptyDescription')}
        action={<Button variant="secondary" onClick={() => setActiveView('chat')}>{t('usage:startChat')}</Button>}
      />
    )
  } else {
    const cur = data.summary.current
    const prev = data.summary.previous
    const tokens = cur.input_tokens + cur.output_tokens
    const rate = errorRate(cur.llm_calls || cur.requests, cur.errors)
    const latency = formatLatency(cur.avg_duration_ms, locale)
    const vsPrevious = (pct: string) => t('usage:vsPrevious', { change: pct })
    body = (
      <div className={cn('space-y-4', loading && 'opacity-60')}>
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
          <Kpi label={t('usage:kpi.requests')} value={formatInteger(cur.requests, locale)} change={delta(cur.requests, prev.requests)} previousLabel={vsPrevious} />
          <Kpi label={t('usage:kpi.tokens')} value={formatCompactNumber(tokens, locale)} change={delta(tokens, prev.input_tokens + prev.output_tokens)} previousLabel={vsPrevious} />
          <Kpi label={t('usage:kpi.cost')} value={formatCost(cur.cost, locale)} change={delta(cur.cost, prev.cost)} previousLabel={vsPrevious} />
          <Kpi label={t('usage:kpi.users')} value={formatInteger(cur.unique_users, locale)} change={delta(cur.unique_users, prev.unique_users)} previousLabel={vsPrevious} />
        </div>
        <UsageTokenChart points={data.points} range={range} locale={locale} />
        <dl className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          {[
            [t('usage:detail.llmCalls'), formatInteger(cur.llm_calls, locale)],
            [t('usage:detail.toolCalls'), formatInteger(cur.tool_calls, locale)],
            [t('usage:detail.errorRate'), rate === null ? '—' : formatPercent(rate, locale)],
            [t('usage:detail.avgDuration'), `${latency.value} ${t(`usage:units.${latency.unit}`)}`],
          ].map(([label, value]) => (
            <div key={label} className="rounded-lg border border-border-subtle bg-surface-raised px-4 py-3">
              <dt className="text-caption text-ink-muted">{label}</dt>
              <dd className="m-0 text-body tabular-nums text-ink">{value}</dd>
            </div>
          ))}
        </dl>
      </div>
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader title={t('usage:title')} description={t('usage:description')} actions={rangeControl} />
      <div className={cn('min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-4', disconnected && 'opacity-60')}>
        {body}
      </div>
    </div>
  )
}

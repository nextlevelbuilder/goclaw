import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '../ui'
import { SectionCard } from './section-card'
import type { ToolAggregate, RetrievalAggregate } from '../../types/evolution'

interface EvolutionMetricsDisplayProps {
  toolAggs: ToolAggregate[]
  retrievalAggs: RetrievalAggregate[]
  loading: boolean
}

/** Rates arrive as fractions (0–1); show them as whole percents. */
function toPercent(fraction: number): number {
  return Math.max(0, Math.min(100, Math.round(fraction * 100)))
}

function formatDuration(ms: number): string {
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)} s` : `${Math.round(ms)} ms`
}

function RateBar({ percent, tone, label }: { percent: number; tone: 'success' | 'accent'; label: string }) {
  return (
    <div className="flex items-center gap-2">
      <div
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent}
        aria-label={label}
        className="h-2 w-24 overflow-hidden rounded-pill bg-surface-hover"
      >
        <div className={tone === 'success' ? 'h-full bg-success' : 'h-full bg-accent'} style={{ width: `${percent}%` }} />
      </div>
      <span className="w-10 text-right font-mono text-code text-ink-secondary">{percent}%</span>
    </div>
  )
}

function MetricsTable({ children, head, label }: { children: ReactNode; head: string[]; label: string }) {
  return (
    <div className="overflow-x-auto">
      <table aria-label={label} className="w-full min-w-[480px] text-ui">
        <thead>
          <tr className="border-b border-border-subtle text-left text-overline uppercase text-ink-muted">
            {head.map((h, i) => (
              <th key={h} scope="col" className={i === 0 ? 'py-2 pr-3 font-medium' : 'px-3 py-2 font-medium'}>{h}</th>
            ))}
          </tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  )
}

export function EvolutionMetricsDisplay({ toolAggs, retrievalAggs, loading }: EvolutionMetricsDisplayProps) {
  const { t } = useTranslation('agents')

  if (loading) {
    return (
      <div className="flex flex-col gap-4" aria-busy="true">
        <Skeleton className="h-32 w-full" />
        <Skeleton className="h-32 w-full" />
      </div>
    )
  }

  if (toolAggs.length === 0 && retrievalAggs.length === 0) {
    return (
      <p className="rounded-lg border border-border-subtle bg-surface-raised px-4 py-8 text-center text-ui font-normal text-ink-muted">
        {t('screen.evolution.noMetrics')}
      </p>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      {toolAggs.length > 0 && (
        <SectionCard title={t('screen.evolution.toolPerformance')}>
          <MetricsTable
            label={t('screen.evolution.toolPerformance')}
            head={[t('screen.evolution.colTool'), t('screen.evolution.colCalls'), t('screen.evolution.colSuccess'), t('screen.evolution.colAvgTime')]}
          >
            {toolAggs.map((a) => (
              <tr key={a.tool_name} className="border-b border-border-subtle last:border-0">
                <td className="max-w-[240px] truncate py-2 pr-3 font-mono text-code text-ink">{a.tool_name}</td>
                <td className="px-3 py-2 font-mono text-code text-ink-secondary">{a.call_count}</td>
                <td className="px-3 py-2"><RateBar percent={toPercent(a.success_rate)} tone="success" label={a.tool_name} /></td>
                <td className="px-3 py-2 font-mono text-code text-ink-secondary">{formatDuration(a.avg_duration_ms)}</td>
              </tr>
            ))}
          </MetricsTable>
        </SectionCard>
      )}

      {retrievalAggs.length > 0 && (
        <SectionCard title={t('screen.evolution.retrieval')}>
          <MetricsTable
            label={t('screen.evolution.retrieval')}
            head={[t('screen.evolution.colSource'), t('screen.evolution.colQueries'), t('screen.evolution.colUsed'), t('screen.evolution.colAvgScore')]}
          >
            {retrievalAggs.map((a) => (
              <tr key={a.source} className="border-b border-border-subtle last:border-0">
                <td className="max-w-[240px] truncate py-2 pr-3 font-mono text-code text-ink">{a.source}</td>
                <td className="px-3 py-2 font-mono text-code text-ink-secondary">{a.query_count}</td>
                <td className="px-3 py-2"><RateBar percent={toPercent(a.usage_rate)} tone="accent" label={a.source} /></td>
                <td className="px-3 py-2 font-mono text-code text-ink-secondary">{a.avg_score.toFixed(2)}</td>
              </tr>
            ))}
          </MetricsTable>
        </SectionCard>
      )}
    </div>
  )
}

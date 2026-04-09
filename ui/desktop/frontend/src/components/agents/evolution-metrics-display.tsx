import { useTranslation } from 'react-i18next'
import type { ToolAggregate, RetrievalAggregate } from '../../types/evolution'

interface EvolutionMetricsDisplayProps {
  toolAggs: ToolAggregate[]
  retrievalAggs: RetrievalAggregate[]
  loading: boolean
}

function BarRow({ label, value, color }: { label: string; value: number; color: string }) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-xs w-24 truncate text-text-secondary">{label}</span>
      <div className="flex-1 h-5 bg-surface-tertiary rounded-full overflow-hidden">
        <div className={`h-full rounded-full transition-all ${color}`} style={{ width: `${Math.min(value, 100)}%` }} />
      </div>
      <span className="text-xs text-text-muted w-12 text-right">{value.toFixed(0)}%</span>
    </div>
  )
}

export function EvolutionMetricsDisplay({ toolAggs, retrievalAggs, loading }: EvolutionMetricsDisplayProps) {
  const { t } = useTranslation('agents')
  if (loading) {
    return (
      <div className="space-y-3">
        <div className="h-20 animate-pulse rounded-lg bg-surface-tertiary" />
        <div className="h-20 animate-pulse rounded-lg bg-surface-tertiary" />
      </div>
    )
  }

  const noData = toolAggs.length === 0 && retrievalAggs.length === 0
  if (noData) {
    return <p className="text-xs text-text-muted text-center py-6">{t('detail.evolutionTab.noMetrics')}</p>
  }

  return (
    <div className="space-y-5">
      {toolAggs.length > 0 && (
        <div className="space-y-2">
          <h4 className="text-xs font-semibold text-text-primary">{t('detail.evolutionTab.toolSuccess')}</h4>
          <div className="space-y-1.5">
            {toolAggs.map((a) => (
              <BarRow key={a.tool_name} label={a.tool_name} value={a.success_rate} color="bg-success" />
            ))}
          </div>
        </div>
      )}

      {retrievalAggs.length > 0 && (
        <div className="space-y-2">
          <h4 className="text-xs font-semibold text-text-primary">{t('detail.evolutionTab.retrievalQuality')}</h4>
          <div className="space-y-1.5">
            {retrievalAggs.map((a) => (
              <BarRow key={a.source} label={a.source} value={a.avg_score * 100} color="bg-accent" />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}

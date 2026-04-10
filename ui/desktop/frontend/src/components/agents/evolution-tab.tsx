import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useV3Flags } from '../../hooks/use-v3-flags'
import { useEvolutionMetrics } from '../../hooks/use-evolution-metrics'
import { useEvolutionSuggestions } from '../../hooks/use-evolution-suggestions'
import { EvolutionMetricsDisplay } from './evolution-metrics-display'
import { EvolutionSuggestionsList } from './evolution-suggestions-list'
import { EvolutionGuardrailsCard } from './evolution-guardrails-card'
import type { AdaptationGuardrails } from '../../types/evolution'

interface EvolutionTabProps {
  agentId: string
  agentOtherConfig?: Record<string, unknown> | null
}

const DEFAULT_GUARDRAILS: AdaptationGuardrails = {
  max_delta_per_cycle: 0.1,
  min_data_points: 100,
  rollback_on_drop_pct: 20,
  locked_params: [],
}

const TIME_RANGES = ['7d', '30d', '90d'] as const
type TimeRange = (typeof TIME_RANGES)[number]

export function EvolutionTab({ agentId, agentOtherConfig }: EvolutionTabProps) {
  const { t } = useTranslation('agents')
  const { flags, loading: flagsLoading } = useV3Flags(agentId)
  const [timeRange, setTimeRange] = useState<TimeRange>('7d')
  const { toolAggs, retrievalAggs, loading: metricsLoading } = useEvolutionMetrics(agentId, timeRange)
  const { suggestions, loading: suggestionsLoading, updateStatus } = useEvolutionSuggestions(agentId)

  const guardrails = (agentOtherConfig?.evolution_guardrails as AdaptationGuardrails) ?? DEFAULT_GUARDRAILS

  // Empty state when metrics not enabled
  if (!flagsLoading && !flags.self_evolution_metrics) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-center space-y-3">
        <span className="text-4xl">✨</span>
        <h3 className="text-sm font-medium text-text-primary">{t('detail.evolutionTab.notEnabled')}</h3>
        <p className="text-xs text-text-muted max-w-sm">{t('detail.evolutionTab.notEnabledHint')}</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Time range selector */}
      <div className="flex items-center gap-1">
        {TIME_RANGES.map((r) => (
          <button
            key={r}
            onClick={() => setTimeRange(r)}
            className={[
              'px-3 py-1 text-xs rounded-lg transition-colors',
              timeRange === r
                ? 'bg-accent text-white'
                : 'bg-surface-tertiary text-text-muted hover:text-text-primary',
            ].join(' ')}
          >
            {r}
          </button>
        ))}
      </div>

      <EvolutionMetricsDisplay toolAggs={toolAggs} retrievalAggs={retrievalAggs} loading={metricsLoading} />
      <hr className="border-border" />
      <EvolutionSuggestionsList suggestions={suggestions} loading={suggestionsLoading} onUpdateStatus={updateStatus} />
      <hr className="border-border" />
      <EvolutionGuardrailsCard guardrails={guardrails} />
    </div>
  )
}

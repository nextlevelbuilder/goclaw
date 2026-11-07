import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useV3Flags } from '../../hooks/use-v3-flags'
import { useEvolutionMetrics } from '../../hooks/use-evolution-metrics'
import { useEvolutionSuggestions } from '../../hooks/use-evolution-suggestions'
import { Button, EmptyState, ErrorState, SegmentedControl, Skeleton } from '../ui'
import { EvolutionMetricsDisplay } from './evolution-metrics-display'
import { EvolutionSuggestionsList } from './evolution-suggestions-list'
import { EvolutionGuardrailsCard } from './evolution-guardrails-card'
import type { AdaptationGuardrails } from '../../types/evolution'

interface EvolutionTabProps {
  agentId: string
  agentOtherConfig?: Record<string, unknown> | null
  readOnly?: boolean
}

const DEFAULT_GUARDRAILS: AdaptationGuardrails = {
  max_delta_per_cycle: 0.1,
  min_data_points: 100,
  rollback_on_drop_pct: 20,
  locked_params: [],
}

const TIME_RANGES = ['7d', '30d', '90d'] as const
type TimeRange = (typeof TIME_RANGES)[number]

export function EvolutionTab({ agentId, agentOtherConfig, readOnly = false }: EvolutionTabProps) {
  const { t } = useTranslation('agents')
  const { flags, loading: flagsLoading, error: flagsError, refetch: refetchFlags, toggleFlag } = useV3Flags(agentId)
  const [timeRange, setTimeRange] = useState<TimeRange>('7d')
  const { toolAggs, retrievalAggs, loading: metricsLoading, error: metricsError, refetch: refetchMetrics } = useEvolutionMetrics(agentId, timeRange)
  const { suggestions, loading: suggestionsLoading, error: suggestionsError, refetch: refetchSuggestions, updateStatus } = useEvolutionSuggestions(agentId)

  const guardrails = { ...DEFAULT_GUARDRAILS, ...((agentOtherConfig?.evolution_guardrails as Partial<AdaptationGuardrails> | undefined) ?? {}) }

  if (flagsLoading) {
    return (
      <div className="flex flex-col gap-4" aria-busy="true">
        <Skeleton className="h-10 w-full" />
        <Skeleton className="h-40 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    )
  }

  if (flagsError) {
    return (
      <ErrorState
        title={t('screen.evolution.loadError')}
        description={t('screen.evolution.loadErrorHint')}
        onRetry={() => { void refetchFlags() }}
      />
    )
  }

  if (!flags.self_evolution_metrics) {
    return (
      <EmptyState
        icon={<span aria-hidden="true" className="text-title-lg">✨</span>}
        title={t('screen.evolution.offTitle')}
        description={
          <>
            {t('screen.evolution.offDescription')}
            <span className="mt-2 block">{t('screen.evolution.offHint', { count: guardrails.min_data_points })}</span>
          </>
        }
        action={
          <Button variant="primary" onClick={() => { void toggleFlag('self_evolution_metrics', true) }} disabled={readOnly}>
            {t('screen.evolution.turnOn')}
          </Button>
        }
      />
    )
  }

  const loadFailed = metricsError || suggestionsError

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <p className="min-w-0 flex-1 rounded-md bg-info-subtle px-3 py-2 text-caption text-info">
          {t('screen.evolution.infoOn', { count: guardrails.min_data_points })}
        </p>
        <SegmentedControl
          aria-label={t('screen.evolution.range')}
          size="sm"
          value={timeRange}
          onValueChange={setTimeRange}
          options={TIME_RANGES.map((r) => ({ value: r, label: t('screen.evolution.lastDays', { count: Number.parseInt(r, 10) }) }))}
        />
      </div>

      {loadFailed ? (
        <ErrorState
          title={t('screen.evolution.loadError')}
          description={t('screen.evolution.loadErrorHint')}
          onRetry={() => { void refetchMetrics(); void refetchSuggestions() }}
        />
      ) : (
        <>
          <EvolutionMetricsDisplay toolAggs={toolAggs} retrievalAggs={retrievalAggs} loading={metricsLoading} />
          <EvolutionSuggestionsList suggestions={suggestions} loading={suggestionsLoading} readOnly={readOnly} onUpdateStatus={updateStatus} />
        </>
      )}
      <EvolutionGuardrailsCard guardrails={guardrails} />
    </div>
  )
}

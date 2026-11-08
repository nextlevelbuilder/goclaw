import { useTranslation } from 'react-i18next'
import { SectionCard } from './section-card'
import type { AdaptationGuardrails } from '../../types/evolution'

interface EvolutionGuardrailsCardProps {
  guardrails: AdaptationGuardrails
}

export function EvolutionGuardrailsCard({ guardrails }: EvolutionGuardrailsCardProps) {
  const { t } = useTranslation('agents')
  // max_delta_per_cycle is a fraction (0.1 = 10%); rollback_on_drop_pct is already a percentage.
  const stats = [
    { label: t('screen.evolution.maxDelta'), value: `${Math.round(guardrails.max_delta_per_cycle * 100)}%` },
    { label: t('screen.evolution.minDataPoints'), value: String(guardrails.min_data_points) },
    { label: t('screen.evolution.rollbackDrop'), value: `${guardrails.rollback_on_drop_pct}%` },
  ]
  const locked = guardrails.locked_params ?? []

  return (
    <SectionCard title={t('screen.evolution.guardrails')} subtitle={t('screen.evolution.guardrailsHint')}>
      <dl className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        {stats.map((s) => (
          <div key={s.label} className="rounded-lg border border-border-subtle bg-surface-base p-3">
            <dd className="font-mono text-title text-ink">{s.value}</dd>
            <dt className="text-caption font-normal text-ink-muted">{s.label}</dt>
          </div>
        ))}
      </dl>
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="text-caption font-normal text-ink-muted">{t('screen.evolution.locked')}:</span>
        {locked.length === 0 ? (
          <span className="text-caption font-normal text-ink-muted">{t('screen.evolution.lockedNone')}</span>
        ) : (
          locked.map((p) => (
            <code key={p} className="rounded-sm bg-surface-hover px-1.5 py-0.5 font-mono text-code text-ink-secondary">{p}</code>
          ))
        )}
      </div>
      <p className="text-caption font-normal text-ink-muted">{t('screen.evolution.lockedHint')}</p>
    </SectionCard>
  )
}

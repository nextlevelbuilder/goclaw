import { useTranslation } from 'react-i18next'
import type { AdaptationGuardrails } from '../../types/evolution'

interface EvolutionGuardrailsCardProps {
  guardrails: AdaptationGuardrails
}

export function EvolutionGuardrailsCard({ guardrails }: EvolutionGuardrailsCardProps) {
  const { t } = useTranslation('agents')
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <svg className="w-4 h-4 text-text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
        </svg>
        <h4 className="text-xs font-semibold text-text-primary">{t('detail.evolutionTab.guardrails')}</h4>
      </div>
      <div className="grid grid-cols-3 gap-3">
        <div className="rounded-lg border border-border p-2.5 text-center">
          <p className="text-sm font-semibold text-text-primary">{guardrails.max_delta_per_cycle}</p>
          <p className="text-[10px] text-text-muted">{t('detail.evolutionTab.maxDelta')}</p>
        </div>
        <div className="rounded-lg border border-border p-2.5 text-center">
          <p className="text-sm font-semibold text-text-primary">{guardrails.min_data_points}</p>
          <p className="text-[10px] text-text-muted">{t('detail.evolutionTab.minDataPoints')}</p>
        </div>
        <div className="rounded-lg border border-border p-2.5 text-center">
          <p className="text-sm font-semibold text-text-primary">{guardrails.rollback_on_drop_pct}%</p>
          <p className="text-[10px] text-text-muted">{t('detail.evolutionTab.rollbackDrop')}</p>
        </div>
      </div>
      {guardrails.locked_params.length > 0 && (
        <div className="flex flex-wrap gap-1">
          <span className="text-[10px] text-text-muted mr-1">Locked:</span>
          {guardrails.locked_params.map((p) => (
            <span key={p} className="text-[9px] px-1.5 py-0.5 rounded bg-surface-tertiary text-text-muted">{p}</span>
          ))}
        </div>
      )}
    </div>
  )
}

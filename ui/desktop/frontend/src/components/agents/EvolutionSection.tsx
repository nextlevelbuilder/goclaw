import { useTranslation } from 'react-i18next'
import { Switch } from '../common/Switch'

interface EvolutionSectionProps {
  selfEvolve: boolean
  onSelfEvolveChange: (v: boolean) => void
}

export function EvolutionSection({ selfEvolve, onSelfEvolveChange }: EvolutionSectionProps) {
  const { t } = useTranslation('agents')
  return (
    <div className="space-y-4">
      <h3 className="text-sm font-semibold text-text-primary">{t('detail.evolution')}</h3>

      {/* Self-evolve toggle */}
      <div className="flex items-center justify-between rounded-lg border border-border p-3">
        <div className="space-y-0.5">
          <div className="flex items-center gap-2">
            <span className="text-sm">✨</span>
            <span className="text-xs font-medium text-text-primary">{t('general.selfEvolution')}</span>
          </div>
          <p className="text-[11px] text-text-muted">
            {t('general.selfEvolutionLabel')}
          </p>
        </div>
        <Switch checked={selfEvolve} onCheckedChange={onSelfEvolveChange} />
      </div>

      {selfEvolve && (
        <div className="rounded-lg border border-orange-500/20 bg-orange-500/5 p-3">
          <p className="text-[11px] text-orange-600 dark:text-orange-400">
            {t('general.selfEvolutionInfo')}
          </p>
        </div>
      )}
    </div>
  )
}

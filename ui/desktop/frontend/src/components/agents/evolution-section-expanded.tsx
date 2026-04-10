import { useTranslation } from 'react-i18next'
import { Switch } from '../common/Switch'
import { useV3Flags } from '../../hooks/use-v3-flags'

interface EvolutionSectionExpandedProps {
  agentId: string
  selfEvolve: boolean
  onSelfEvolveChange: (v: boolean) => void
  skillLearning: boolean
  onSkillLearningChange: (v: boolean) => void
  skillNudgeInterval: number
  onSkillNudgeIntervalChange: (v: number) => void
}

export function EvolutionSectionExpanded({
  agentId, selfEvolve, onSelfEvolveChange,
  skillLearning, onSkillLearningChange,
  skillNudgeInterval, onSkillNudgeIntervalChange,
}: EvolutionSectionExpandedProps) {
  const { t } = useTranslation('agents')
  const { flags, loading, toggleFlag } = useV3Flags(agentId)

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
          <p className="text-[11px] text-text-muted">{t('general.selfEvolutionLabel')}</p>
        </div>
        <Switch checked={selfEvolve} onCheckedChange={onSelfEvolveChange} />
      </div>

      {selfEvolve && (
        <div className="rounded-lg border border-orange-500/20 bg-orange-500/5 p-3">
          <p className="text-[11px] text-orange-600 dark:text-orange-400">{t('general.selfEvolutionInfo')}</p>
        </div>
      )}

      {/* Skill learning toggle */}
      <div className="flex items-center justify-between rounded-lg border border-border p-3">
        <div className="space-y-0.5">
          <div className="flex items-center gap-2">
            <span className="text-sm">📚</span>
            <span className="text-xs font-medium text-text-primary">{t('general.skillLearning')}</span>
          </div>
          <p className="text-[11px] text-text-muted">{t('general.skillLearningLabel')}</p>
        </div>
        <Switch checked={skillLearning} onCheckedChange={onSkillLearningChange} />
      </div>

      {skillLearning && (
        <div className="rounded-lg border border-border p-3 space-y-2">
          <label className="text-[11px] font-medium text-text-secondary">{t('general.skillNudgeIntervalLabel')}</label>
          <input
            type="number"
            min={0}
            value={skillNudgeInterval}
            onChange={(e) => onSkillNudgeIntervalChange(Number(e.target.value) || 0)}
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
          />
          <p className="text-[10px] text-text-muted">{t('general.skillNudgeIntervalHint')}</p>
        </div>
      )}

      {/* V3 engine flags */}
      <div className="space-y-2">
        <div className="flex items-center gap-2">
          <h4 className="text-xs font-semibold text-text-primary">{t('general.v3Flags')}</h4>
          <span className="text-[9px] px-1.5 py-0.5 rounded bg-surface-tertiary text-text-muted">{t('general.v3FlagsHint')}</span>
        </div>

        {loading ? (
          <div className="h-16 animate-pulse rounded-lg bg-surface-tertiary" />
        ) : (
          <div className="space-y-2">
            <div className="flex items-center justify-between rounded-lg border border-border p-3">
              <div className="space-y-0.5">
                <span className="text-xs font-medium text-text-primary">{t('general.evolutionMetrics')}</span>
                <p className="text-[11px] text-text-muted">{t('general.evolutionMetricsLabel')}</p>
              </div>
              <Switch
                checked={flags.self_evolution_metrics}
                onCheckedChange={(v) => toggleFlag('self_evolution_metrics', v)}
              />
            </div>
            <div className="flex items-center justify-between rounded-lg border border-border p-3">
              <div className="space-y-0.5">
                <span className="text-xs font-medium text-text-primary">{t('general.evolutionSuggestions')}</span>
                <p className="text-[11px] text-text-muted">{t('general.evolutionSuggestionsLabel')}</p>
              </div>
              <Switch
                checked={flags.self_evolution_suggestions}
                onCheckedChange={(v) => toggleFlag('self_evolution_suggestions', v)}
              />
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

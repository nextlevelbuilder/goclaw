import { useTranslation } from 'react-i18next'
import { Switch } from '../ui'
import { useV3Flags } from '../../hooks/use-v3-flags'
import { Input, Skeleton } from '../ui'

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
      <h3 className="text-ui font-semibold text-ink">{t('detail.evolution')}</h3>

      {/* Self-evolve toggle */}
      <div className="flex items-center justify-between rounded-lg border border-border-subtle p-3">
        <div className="space-y-0.5">
          <div className="flex items-center gap-2">
            <span className="text-ui">✨</span>
            <span className="text-caption font-medium text-ink">{t('general.selfEvolution')}</span>
          </div>
          <p className="text-caption text-ink-muted">{t('general.selfEvolutionLabel')}</p>
        </div>
        <Switch checked={selfEvolve} onCheckedChange={onSelfEvolveChange} />
      </div>

      {selfEvolve && (
        <div className="rounded-lg border border-border-subtle bg-warning-subtle p-3">
          <p className="text-caption text-warning">{t('general.selfEvolutionInfo')}</p>
        </div>
      )}

      {/* Skill learning toggle */}
      <div className="flex items-center justify-between rounded-lg border border-border-subtle p-3">
        <div className="space-y-0.5">
          <div className="flex items-center gap-2">
            <span className="text-ui">📚</span>
            <span className="text-caption font-medium text-ink">{t('general.skillLearning')}</span>
          </div>
          <p className="text-caption text-ink-muted">{t('general.skillLearningLabel')}</p>
        </div>
        <Switch checked={skillLearning} onCheckedChange={onSkillLearningChange} />
      </div>

      {skillLearning && (
        <div className="rounded-lg border border-border-subtle p-3 space-y-2">
          <label className="text-caption font-medium text-ink-secondary">{t('general.skillNudgeIntervalLabel')}</label>
          <Input
            type="number"
            min={0}
            value={skillNudgeInterval}
            onChange={(e) => onSkillNudgeIntervalChange(Number(e.target.value) || 0)}
            className="w-full"
          />
          <p className="text-caption text-ink-muted">{t('general.skillNudgeIntervalHint')}</p>
        </div>
      )}

      {/* V3 engine flags */}
      <div className="space-y-2">
        <div className="flex items-center gap-2">
          <h4 className="text-caption font-semibold text-ink">{t('general.v3Flags')}</h4>
          <span className="text-caption px-1.5 py-0.5 rounded bg-surface-hover text-ink-muted">{t('general.v3FlagsHint')}</span>
        </div>

        {loading ? (
          <Skeleton className="h-16 w-full" />
        ) : (
          <div className="space-y-2">
            <div className="flex items-center justify-between rounded-lg border border-border-subtle p-3">
              <div className="space-y-0.5">
                <span className="text-caption font-medium text-ink">{t('general.evolutionMetrics')}</span>
                <p className="text-caption text-ink-muted">{t('general.evolutionMetricsLabel')}</p>
              </div>
              <Switch
                checked={flags.self_evolution_metrics}
                onCheckedChange={(v) => toggleFlag('self_evolution_metrics', v)}
              />
            </div>
            <div className="flex items-center justify-between rounded-lg border border-border-subtle p-3">
              <div className="space-y-0.5">
                <span className="text-caption font-medium text-ink">{t('general.evolutionSuggestions')}</span>
                <p className="text-caption text-ink-muted">{t('general.evolutionSuggestionsLabel')}</p>
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

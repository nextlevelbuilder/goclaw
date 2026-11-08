import { useTranslation } from 'react-i18next'
import type { ReasoningOverrideMode } from '../../types/agent'

interface ThinkingSectionProps {
  reasoningMode: ReasoningOverrideMode
  thinkingLevel: string
  onReasoningModeChange: (v: ReasoningOverrideMode) => void
  onThinkingLevelChange: (v: string) => void
}

const LEVELS = [
  { key: 'off', label: 'Off', desc: 'No extended thinking' },
  { key: 'low', label: 'Low', desc: '~4K token budget' },
  { key: 'medium', label: 'Medium', desc: '~10-16K token budget' },
  { key: 'high', label: 'High', desc: '~32K token budget' },
] as const

export function ThinkingSection({ reasoningMode, thinkingLevel, onReasoningModeChange, onThinkingLevelChange }: ThinkingSectionProps) {
  const { t } = useTranslation('agents')

  return (
    <div className="space-y-3">
      <h3 className="text-ui font-semibold text-ink">{t('configSections.thinking.title')}</h3>
      <p className="text-caption text-ink-muted">{t('configSections.thinking.description')}</p>

      {/* Inherit / Custom toggle */}
      <div className="flex gap-2">
        {(['inherit', 'custom'] as const).map((m) => (
          <button
            key={m}
            onClick={() => onReasoningModeChange(m)}
            className={[
              'px-3 py-1.5 text-caption rounded-lg border transition-colors',
              reasoningMode === m
                ? 'bg-accent text-on-accent border-accent'
                : 'border-border-subtle text-ink-secondary hover:bg-surface-hover',
            ].join(' ')}
          >
            {m === 'inherit' ? 'Inherit' : 'Custom'}
          </button>
        ))}
      </div>

      {reasoningMode === 'custom' ? (
        <div className="space-y-2">
          <label className="text-caption font-medium text-ink-secondary">{t('configSections.thinking.thinkingLevel')}</label>
          <div className="flex gap-1.5">
            {LEVELS.map((lv) => (
              <button
                key={lv.key}
                onClick={() => onThinkingLevelChange(lv.key)}
                className={[
                  'flex-1 px-2 py-1.5 text-caption rounded-lg border transition-colors text-center',
                  thinkingLevel === lv.key
                    ? 'bg-accent text-on-accent border-accent'
                    : 'border-border-subtle text-ink-secondary hover:bg-surface-hover',
                ].join(' ')}
              >
                {lv.label}
              </button>
            ))}
          </div>
          <p className="text-caption text-ink-muted">
            {LEVELS.find((l) => l.key === thinkingLevel)?.desc}
          </p>
        </div>
      ) : (
        <p className="text-caption text-ink-muted italic">Using provider defaults</p>
      )}
    </div>
  )
}

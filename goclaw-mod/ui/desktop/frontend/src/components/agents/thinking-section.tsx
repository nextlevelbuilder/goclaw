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
      <h3 className="text-sm font-semibold text-text-primary">{t('configSections.thinking.title')}</h3>
      <p className="text-[11px] text-text-muted">{t('configSections.thinking.description')}</p>

      {/* Inherit / Custom toggle */}
      <div className="flex gap-2">
        {(['inherit', 'custom'] as const).map((m) => (
          <button
            key={m}
            onClick={() => onReasoningModeChange(m)}
            className={[
              'px-3 py-1.5 text-xs rounded-lg border transition-colors',
              reasoningMode === m
                ? 'bg-accent text-white border-accent'
                : 'border-border text-text-secondary hover:bg-surface-tertiary',
            ].join(' ')}
          >
            {m === 'inherit' ? 'Inherit' : 'Custom'}
          </button>
        ))}
      </div>

      {reasoningMode === 'custom' ? (
        <div className="space-y-2">
          <label className="text-xs font-medium text-text-secondary">{t('configSections.thinking.thinkingLevel')}</label>
          <div className="flex gap-1.5">
            {LEVELS.map((lv) => (
              <button
                key={lv.key}
                onClick={() => onThinkingLevelChange(lv.key)}
                className={[
                  'flex-1 px-2 py-1.5 text-xs rounded-lg border transition-colors text-center',
                  thinkingLevel === lv.key
                    ? 'bg-accent text-white border-accent'
                    : 'border-border text-text-secondary hover:bg-surface-tertiary',
                ].join(' ')}
              >
                {lv.label}
              </button>
            ))}
          </div>
          <p className="text-[10px] text-text-muted">
            {LEVELS.find((l) => l.key === thinkingLevel)?.desc}
          </p>
        </div>
      ) : (
        <p className="text-[11px] text-text-muted italic">Using provider defaults</p>
      )}
    </div>
  )
}

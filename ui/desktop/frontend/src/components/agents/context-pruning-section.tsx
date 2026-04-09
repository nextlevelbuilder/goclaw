import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ConfigSection } from './config-section'
import { Switch } from '../common/Switch'
import { numOrUndef } from '../../lib/format'
import type { ContextPruningConfig } from '../../types/agent'

interface ContextPruningSectionProps {
  enabled: boolean
  value: ContextPruningConfig
  onToggle: (v: boolean) => void
  onChange: (v: ContextPruningConfig) => void
}

function NumInput({ label, value, hint, step, onChange }: {
  label: string; value?: number; hint?: string; step?: number
  onChange: (v: number | undefined) => void
}) {
  return (
    <div className="space-y-1">
      <label className="text-[11px] font-medium text-text-secondary">{label}</label>
      <input
        type="number"
        value={value ?? ''}
        step={step}
        onChange={(e) => onChange(numOrUndef(e.target.value))}
        className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
      />
      {hint && <p className="text-[10px] text-text-muted">{hint}</p>}
    </div>
  )
}

export function ContextPruningSection({ enabled, value, onToggle, onChange }: ContextPruningSectionProps) {
  const { t } = useTranslation('agents')
  const [showAdvanced, setShowAdvanced] = useState(false)

  const update = (patch: Partial<ContextPruningConfig>) => onChange({ ...value, ...patch })

  return (
    <ConfigSection
      title={t('configSections.contextPruning.title')}
      description={t('configSections.contextPruning.description')}
      enabled={enabled}
      onToggle={onToggle}
    >
      <NumInput
        label={t('configSections.contextPruning.keepLastAssistants')}
        value={value.keepLastAssistants}
        onChange={(v) => update({ keepLastAssistants: v })}
      />

      <button
        onClick={() => setShowAdvanced(!showAdvanced)}
        className="flex items-center gap-1 text-xs text-text-muted hover:text-text-primary transition-colors"
      >
        <svg className={`w-3 h-3 transition-transform ${showAdvanced ? 'rotate-90' : ''}`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
          <polyline points="9 6 15 12 9 18" />
        </svg>
        Advanced
      </button>

      {showAdvanced && (
        <div className="space-y-3 border-l-2 border-border pl-3">
          <div className="grid grid-cols-2 gap-3">
            <NumInput label={t('configSections.contextPruning.softTrimRatio')} value={value.softTrimRatio} step={0.05} onChange={(v) => update({ softTrimRatio: v })} />
            <NumInput label={t('configSections.contextPruning.hardClearRatio')} value={value.hardClearRatio} step={0.05} onChange={(v) => update({ hardClearRatio: v })} />
          </div>
          <div className="grid grid-cols-3 gap-2">
            <NumInput label={t('configSections.contextPruning.maxChars')} value={value.softTrim?.maxChars} onChange={(v) => update({ softTrim: { ...value.softTrim, maxChars: v } })} />
            <NumInput label={t('configSections.contextPruning.headChars')} value={value.softTrim?.headChars} onChange={(v) => update({ softTrim: { ...value.softTrim, headChars: v } })} />
            <NumInput label={t('configSections.contextPruning.tailChars')} value={value.softTrim?.tailChars} onChange={(v) => update({ softTrim: { ...value.softTrim, tailChars: v } })} />
          </div>
          <div className="flex items-center justify-between rounded-lg border border-border p-2.5">
            <span className="text-xs text-text-secondary">{t('configSections.contextPruning.hardClear')}</span>
            <Switch
              checked={value.hardClear?.enabled ?? false}
              onCheckedChange={(v) => update({ hardClear: { enabled: v } })}
            />
          </div>
        </div>
      )}
    </ConfigSection>
  )
}

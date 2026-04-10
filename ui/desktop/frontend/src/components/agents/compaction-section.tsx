import { useTranslation } from 'react-i18next'
import { Switch } from '../common/Switch'
import { numOrUndef } from '../../lib/format'
import type { CompactionConfig } from '../../types/agent'

interface CompactionSectionProps {
  value: CompactionConfig
  onChange: (v: CompactionConfig) => void
}

export function CompactionSection({ value, onChange }: CompactionSectionProps) {
  const { t } = useTranslation('agents')
  const update = (patch: Partial<CompactionConfig>) => onChange({ ...value, ...patch })

  return (
    <div className="space-y-3">
      <div className="space-y-0.5">
        <h4 className="text-xs font-semibold text-text-primary">{t('configSections.compaction.title')}</h4>
        <p className="text-[11px] text-text-muted">{t('configSections.compaction.description')}</p>
      </div>

      <div className="rounded-lg border border-border p-3 space-y-3">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="space-y-1">
            <label className="text-[11px] font-medium text-text-secondary">{t('configSections.compaction.maxHistoryShare')}</label>
            <input
              type="number"
              step={0.05}
              min={0}
              max={1}
              value={value.maxHistoryShare ?? ''}
              onChange={(e) => update({ maxHistoryShare: numOrUndef(e.target.value) })}
              className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
            />
          </div>
          <div className="space-y-1">
            <label className="text-[11px] font-medium text-text-secondary">{t('configSections.compaction.keepLastMessages')}</label>
            <input
              type="number"
              min={0}
              value={value.keepLastMessages ?? ''}
              onChange={(e) => update({ keepLastMessages: numOrUndef(e.target.value) })}
              className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
            />
          </div>
        </div>
        <div className="flex items-center justify-between rounded-lg border border-border p-2.5">
          <div className="space-y-0.5">
            <span className="text-xs font-medium text-text-primary">{t('configSections.compaction.memoryFlush')}</span>
            <p className="text-[10px] text-text-muted">{t('configSections.compaction.memoryFlushTip')}</p>
          </div>
          <Switch
            checked={value.memoryFlush?.enabled ?? false}
            onCheckedChange={(v) => update({ memoryFlush: { ...value.memoryFlush, enabled: v } })}
          />
        </div>
      </div>
    </div>
  )
}

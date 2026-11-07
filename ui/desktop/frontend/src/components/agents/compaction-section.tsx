import { useTranslation } from 'react-i18next'
import { Switch } from '../ui'
import { numOrUndef } from '../../lib/format'
import type { CompactionConfig } from '../../types/agent'
import { Input } from '../ui'

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
        <h4 className="text-caption font-semibold text-ink">{t('configSections.compaction.title')}</h4>
        <p className="text-caption text-ink-muted">{t('configSections.compaction.description')}</p>
      </div>

      <div className="rounded-lg border border-border-subtle p-3 space-y-3">
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div className="space-y-1">
            <label className="text-caption font-medium text-ink-secondary">{t('configSections.compaction.maxHistoryShare')}</label>
            <Input
              type="number"
              step={0.05}
              min={0}
              max={1}
              value={value.maxHistoryShare ?? ''}
              onChange={(e) => update({ maxHistoryShare: numOrUndef(e.target.value) })}
              className="w-full"
            />
          </div>
          <div className="space-y-1">
            <label className="text-caption font-medium text-ink-secondary">{t('configSections.compaction.keepLastMessages')}</label>
            <Input
              type="number"
              min={0}
              value={value.keepLastMessages ?? ''}
              onChange={(e) => update({ keepLastMessages: numOrUndef(e.target.value) })}
              className="w-full"
            />
          </div>
        </div>
        <div className="flex items-center justify-between rounded-lg border border-border-subtle p-2.5">
          <div className="space-y-0.5">
            <span className="text-caption font-medium text-ink">{t('configSections.compaction.memoryFlush')}</span>
            <p className="text-caption text-ink-muted">{t('configSections.compaction.memoryFlushTip')}</p>
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

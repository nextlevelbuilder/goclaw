import { useTranslation } from 'react-i18next'
import { ConfigSection } from './config-section'
import { numOrUndef } from '../../lib/format'
import type { SubagentsConfig } from '../../types/agent'
import { Input } from '../ui'

interface SubagentsSectionProps {
  enabled: boolean
  value: SubagentsConfig
  onToggle: (v: boolean) => void
  onChange: (v: SubagentsConfig) => void
}

export function SubagentsSection({ enabled, value, onToggle, onChange }: SubagentsSectionProps) {
  const { t } = useTranslation('agents')
  const update = (patch: Partial<SubagentsConfig>) => onChange({ ...value, ...patch })

  return (
    <ConfigSection
      title={t('configSections.subagents.title')}
      description={t('configSections.subagents.description')}
      enabled={enabled}
      onToggle={onToggle}
    >
      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1">
          <label className="text-caption font-medium text-ink-secondary">{t('configSections.subagents.maxConcurrent')}</label>
          <Input
            type="number" min={1}
            value={value.maxConcurrent ?? 20}
            onChange={(e) => update({ maxConcurrent: numOrUndef(e.target.value) ?? 20 })}
            className="w-full"
          />
          <p className="text-caption text-ink-muted">{t('configSections.subagents.maxConcurrentLiteHint')}</p>
        </div>
        <div className="space-y-1">
          <label className="text-caption font-medium text-ink-secondary">{t('configSections.subagents.maxSpawnDepth')}</label>
          <Input
            type="number" min={1} max={1}
            value={value.maxSpawnDepth ?? 1}
            disabled
            className="w-full"
          />
          <p className="text-caption text-ink-muted">Lite limit: 1</p>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1">
          <label className="text-caption font-medium text-ink-secondary">{t('configSections.subagents.maxChildrenPerAgent')}</label>
          <Input
            type="number" min={1}
            value={value.maxChildrenPerAgent ?? ''}
            onChange={(e) => update({ maxChildrenPerAgent: numOrUndef(e.target.value) })}
            className="w-full"
          />
        </div>
        <div className="space-y-1">
          <label className="text-caption font-medium text-ink-secondary">{t('configSections.subagents.archiveAfter')}</label>
          <Input
            type="number" min={0}
            value={value.archiveAfterMinutes ?? ''}
            onChange={(e) => update({ archiveAfterMinutes: numOrUndef(e.target.value) })}
            className="w-full"
          />
        </div>
      </div>

      <div className="space-y-1">
        <label className="text-caption font-medium text-ink-secondary">{t('configSections.subagents.modelOverride')}</label>
        <Input
          type="text"
          value={value.model ?? ''}
          placeholder={t('configSections.subagents.inheritFromAgent')}
          onChange={(e) => update({ model: e.target.value || undefined })}
          className="w-full font-mono"
        />
      </div>
    </ConfigSection>
  )
}

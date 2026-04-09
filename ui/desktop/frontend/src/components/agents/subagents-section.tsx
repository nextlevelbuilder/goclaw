import { useTranslation } from 'react-i18next'
import { ConfigSection } from './config-section'
import { numOrUndef } from '../../lib/format'
import type { SubagentsConfig } from '../../types/agent'

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
          <label className="text-[11px] font-medium text-text-secondary">{t('configSections.subagents.maxConcurrent')}</label>
          <input
            type="number" min={1} max={2}
            value={value.maxConcurrent ?? 2}
            onChange={(e) => update({ maxConcurrent: Math.min(numOrUndef(e.target.value) ?? 2, 2) })}
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
          />
          <p className="text-[10px] text-text-muted">Lite limit: 2</p>
        </div>
        <div className="space-y-1">
          <label className="text-[11px] font-medium text-text-secondary">{t('configSections.subagents.maxSpawnDepth')}</label>
          <input
            type="number" min={1} max={1}
            value={value.maxSpawnDepth ?? 1}
            disabled
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary opacity-60"
          />
          <p className="text-[10px] text-text-muted">Lite limit: 1</p>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div className="space-y-1">
          <label className="text-[11px] font-medium text-text-secondary">{t('configSections.subagents.maxChildrenPerAgent')}</label>
          <input
            type="number" min={1}
            value={value.maxChildrenPerAgent ?? ''}
            onChange={(e) => update({ maxChildrenPerAgent: numOrUndef(e.target.value) })}
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>
        <div className="space-y-1">
          <label className="text-[11px] font-medium text-text-secondary">{t('configSections.subagents.archiveAfter')}</label>
          <input
            type="number" min={0}
            value={value.archiveAfterMinutes ?? ''}
            onChange={(e) => update({ archiveAfterMinutes: numOrUndef(e.target.value) })}
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>
      </div>

      <div className="space-y-1">
        <label className="text-[11px] font-medium text-text-secondary">{t('configSections.subagents.modelOverride')}</label>
        <input
          type="text"
          value={value.model ?? ''}
          placeholder={t('configSections.subagents.inheritFromAgent')}
          onChange={(e) => update({ model: e.target.value || undefined })}
          className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent font-mono"
        />
      </div>
    </ConfigSection>
  )
}

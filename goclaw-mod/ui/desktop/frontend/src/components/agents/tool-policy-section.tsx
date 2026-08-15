import { useTranslation } from 'react-i18next'
import { ConfigSection } from './config-section'
import type { ToolPolicyConfig } from '../../types/agent'

interface ToolPolicySectionProps {
  enabled: boolean
  value: ToolPolicyConfig
  onToggle: (v: boolean) => void
  onChange: (v: ToolPolicyConfig) => void
}

function tagsToArray(s: string): string[] | undefined {
  const trimmed = s.trim()
  if (!trimmed) return undefined
  return trimmed.split(',').map((t) => t.trim()).filter(Boolean)
}

function arrayToTags(arr?: string[]): string {
  return arr?.join(', ') ?? ''
}

export function ToolPolicySection({ enabled, value, onToggle, onChange }: ToolPolicySectionProps) {
  const { t } = useTranslation('agents')
  const update = (patch: Partial<ToolPolicyConfig>) => onChange({ ...value, ...patch })

  return (
    <ConfigSection
      title={t('configSections.toolPolicy.title')}
      description={t('configSections.toolPolicy.description')}
      enabled={enabled}
      onToggle={onToggle}
    >
      <div className="space-y-1">
        <label className="text-[11px] font-medium text-text-secondary">{t('configSections.toolPolicy.profile')}</label>
        <select
          value={value.profile ?? ''}
          onChange={(e) => update({ profile: e.target.value || undefined })}
          className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
        >
          <option value="">full (default)</option>
          <option value="coding">coding</option>
          <option value="messaging">messaging</option>
          <option value="minimal">minimal</option>
        </select>
      </div>

      <div className="space-y-1">
        <label className="text-[11px] font-medium text-text-secondary">{t('configSections.toolPolicy.toolCallPrefix')}</label>
        <input
          type="text"
          value={value.toolCallPrefix ?? ''}
          onChange={(e) => update({ toolCallPrefix: e.target.value || undefined })}
          className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary font-mono focus:outline-none focus:ring-1 focus:ring-accent"
        />
        <p className="text-[10px] text-text-muted">{t('configSections.toolPolicy.toolCallPrefixHint')}</p>
      </div>

      <div className="space-y-1">
        <label className="text-[11px] font-medium text-text-secondary">{t('configSections.toolPolicy.allow')}</label>
        <input
          type="text"
          value={arrayToTags(value.allow)}
          placeholder="Comma-separated tool names"
          onChange={(e) => update({ allow: tagsToArray(e.target.value) })}
          className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
        />
      </div>

      <div className="space-y-1">
        <label className="text-[11px] font-medium text-text-secondary">{t('configSections.toolPolicy.deny')}</label>
        <input
          type="text"
          value={arrayToTags(value.deny)}
          placeholder="Comma-separated tool names"
          onChange={(e) => update({ deny: tagsToArray(e.target.value) })}
          className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
        />
      </div>

      <div className="space-y-1">
        <label className="text-[11px] font-medium text-text-secondary">{t('configSections.toolPolicy.alsoAllow')}</label>
        <input
          type="text"
          value={arrayToTags(value.alsoAllow)}
          placeholder="Comma-separated tool names"
          onChange={(e) => update({ alsoAllow: tagsToArray(e.target.value) })}
          className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary focus:outline-none focus:ring-1 focus:ring-accent"
        />
      </div>
    </ConfigSection>
  )
}

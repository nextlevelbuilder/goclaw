import { useTranslation } from 'react-i18next'
import { ConfigSection } from './config-section'
import type { ToolPolicyConfig } from '../../types/agent'
import { Input, Select } from '../ui'

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
        <label className="text-caption font-medium text-ink-secondary">{t('configSections.toolPolicy.profile')}</label>
        <Select
          value={value.profile ?? ''}
          onChange={(e) => update({ profile: e.target.value || undefined })}
          className="w-full"
        >
          <option value="">full (default)</option>
          <option value="coding">coding</option>
          <option value="messaging">messaging</option>
          <option value="minimal">minimal</option>
        </Select>
      </div>

      <div className="space-y-1">
        <label className="text-caption font-medium text-ink-secondary">{t('configSections.toolPolicy.toolCallPrefix')}</label>
        <Input
          type="text"
          value={value.toolCallPrefix ?? ''}
          onChange={(e) => update({ toolCallPrefix: e.target.value || undefined })}
          className="w-full font-mono"
        />
        <p className="text-caption text-ink-muted">{t('configSections.toolPolicy.toolCallPrefixHint')}</p>
      </div>

      <div className="space-y-1">
        <label className="text-caption font-medium text-ink-secondary">{t('configSections.toolPolicy.allow')}</label>
        <Input
          type="text"
          value={arrayToTags(value.allow)}
          placeholder="Comma-separated tool names"
          onChange={(e) => update({ allow: tagsToArray(e.target.value) })}
          className="w-full"
        />
      </div>

      <div className="space-y-1">
        <label className="text-caption font-medium text-ink-secondary">{t('configSections.toolPolicy.deny')}</label>
        <Input
          type="text"
          value={arrayToTags(value.deny)}
          placeholder="Comma-separated tool names"
          onChange={(e) => update({ deny: tagsToArray(e.target.value) })}
          className="w-full"
        />
      </div>

      <div className="space-y-1">
        <label className="text-caption font-medium text-ink-secondary">{t('configSections.toolPolicy.alsoAllow')}</label>
        <Input
          type="text"
          value={arrayToTags(value.alsoAllow)}
          placeholder="Comma-separated tool names"
          onChange={(e) => update({ alsoAllow: tagsToArray(e.target.value) })}
          className="w-full"
        />
      </div>
    </ConfigSection>
  )
}

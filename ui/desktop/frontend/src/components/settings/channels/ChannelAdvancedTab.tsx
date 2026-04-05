import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { ChannelFields } from './channel-field-renderer'
import { configSchema, NETWORK_KEYS, LIMITS_KEYS, STREAMING_KEYS, BEHAVIOR_KEYS, ACCESS_KEYS } from './channel-schemas'
import type { ChannelInstanceData } from '../../../types/channel'

interface ChannelAdvancedTabProps {
  instance: ChannelInstanceData
  onUpdate: (updates: Record<string, unknown>) => Promise<void>
}

const ESSENTIAL_KEYS = new Set(['dm_policy', 'group_policy', 'require_mention', 'mention_mode'])

function getAdvancedFields(channelType: string) {
  const allFields = configSchema[channelType] ?? []
  const advanced = allFields.filter((f) => !ESSENTIAL_KEYS.has(f.key))
  return {
    network: advanced.filter((f) => NETWORK_KEYS.has(f.key)),
    limits: advanced.filter((f) => LIMITS_KEYS.has(f.key)),
    streaming: advanced.filter((f) => STREAMING_KEYS.has(f.key)),
    behavior: advanced.filter((f) => BEHAVIOR_KEYS.has(f.key)),
    access: advanced.filter((f) => ACCESS_KEYS.has(f.key)),
  }
}

function deriveInitialValues(instance: ChannelInstanceData): Record<string, unknown> {
  const config = (instance.config ?? {}) as Record<string, unknown>
  const { groups: _groups, ...rest } = config
  return Object.fromEntries(
    Object.entries(rest).filter(([k]) => !ESSENTIAL_KEYS.has(k)),
  )
}

export function ChannelAdvancedTab({ instance, onUpdate }: ChannelAdvancedTabProps) {
  const { t } = useTranslation('channels')
  const [values, setValues] = useState<Record<string, unknown>>({})
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    setValues(deriveInitialValues(instance))
  }, [instance])

  const handleChange = useCallback((key: string, value: unknown) => {
    setValues((prev) => ({ ...prev, [key]: value }))
  }, [])

  const fields = getAdvancedFields(instance.channel_type)

  const handleSave = async () => {
    setSaving(true)
    try {
      const existingConfig = (instance.config ?? {}) as Record<string, unknown>
      const cleanAdvanced = Object.fromEntries(
        Object.entries(values).filter(([, v]) => v !== undefined && v !== '' && v !== null),
      )
      const merged = { ...existingConfig, ...cleanAdvanced }
      await onUpdate({ config: merged })
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false)
    }
  }

  const sections = [
    { key: 'network', label: t('detail.network'), desc: t('detail.networkDesc'), fields: fields.network },
    { key: 'limits', label: t('detail.limits'), desc: t('detail.limitsDesc'), fields: fields.limits },
    { key: 'streaming', label: t('detail.streaming'), desc: t('detail.streamingDesc'), fields: fields.streaming },
    { key: 'behavior', label: t('detail.behavior'), desc: t('detail.behaviorDesc'), fields: fields.behavior },
    { key: 'access', label: t('detail.accessControl'), desc: t('detail.accessControlDesc'), fields: fields.access },
  ].filter((s) => s.fields.length > 0)

  if (sections.length === 0) {
    return <p className="text-xs text-text-muted py-4">{t('detail.noAdvanced')}</p>
  }

  return (
    <div className="space-y-5">
      {sections.map((s) => (
        <section key={s.key} className="space-y-3 rounded-lg border border-border p-4">
          <div>
            <h3 className="text-xs font-semibold text-text-secondary">{s.label}</h3>
            <p className="text-[11px] text-text-muted mt-0.5">{s.desc}</p>
          </div>
          <ChannelFields fields={s.fields} values={values} onChange={handleChange} idPrefix={`adv-${s.key}`} contextValues={values} />
        </section>
      ))}

      <button
        onClick={handleSave}
        disabled={saving}
        className="px-4 py-1.5 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-50 cursor-pointer"
      >
        {saving ? t('form.saving') : t('detail.saveConfig')}
      </button>
    </div>
  )
}

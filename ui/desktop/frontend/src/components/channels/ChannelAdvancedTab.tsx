import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '../ui'
import { ChannelFields } from './channel-field-renderer'
import { configSchema, NETWORK_KEYS, LIMITS_KEYS, STREAMING_KEYS, BEHAVIOR_KEYS, ACCESS_KEYS } from './channel-schemas'
import { normalizeReasoningDeliveryConfig } from './reasoning-delivery-config'
import type { ChannelInstanceData } from '../../types/channel'

interface ChannelAdvancedTabProps {
  instance: ChannelInstanceData
  /** Disables every control while the gateway is unreachable. */
  readOnly?: boolean
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
  const config = normalizeReasoningDeliveryConfig((instance.config ?? {}) as Record<string, unknown>)
  const { groups: _groups, ...rest } = config
  return Object.fromEntries(
    Object.entries(rest).filter(([k]) => !ESSENTIAL_KEYS.has(k)),
  )
}

export function ChannelAdvancedTab({ instance, readOnly = false, onUpdate }: ChannelAdvancedTabProps) {
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
      const merged = normalizeReasoningDeliveryConfig({ ...existingConfig, ...cleanAdvanced })
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
    return <p className="py-4 text-ui text-ink-muted">{t('detail.noAdvanced')}</p>
  }

  return (
    <fieldset disabled={readOnly} className="m-0 flex min-w-0 flex-col gap-4 border-0 p-0">
      {sections.map((s) => (
        <section key={s.key} className="flex flex-col gap-4 rounded-lg border border-border-subtle bg-surface-base p-4">
          <div>
            <h3 className="text-ui font-medium text-ink">{s.label}</h3>
            <p className="mt-0.5 text-caption text-ink-muted">{s.desc}</p>
          </div>
          <ChannelFields fields={s.fields} values={values} onChange={handleChange} idPrefix={`adv-${s.key}`} contextValues={values} />
        </section>
      ))}

      <div className="flex justify-end">
        <Button variant="primary" onClick={() => { void handleSave() }} loading={saving} disabled={readOnly}>
          {saving ? t('form.saving') : t('detail.saveConfig')}
        </Button>
      </div>
    </fieldset>
  )
}

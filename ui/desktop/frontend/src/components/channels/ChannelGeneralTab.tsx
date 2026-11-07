import { useState, useCallback, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Field, Input, Select, Switch } from '../ui'
import { ChannelFields } from './channel-field-renderer'
import { configSchema, ESSENTIAL_CONFIG_KEYS } from './channel-schemas'
import type { ChannelInstanceData } from '../../types/channel'
import type { AgentData } from '../../types/agent'

interface ChannelGeneralTabProps {
  instance: ChannelInstanceData
  agents: AgentData[]
  /** Disables every control while the gateway is unreachable. */
  readOnly?: boolean
  onUpdate: (updates: Record<string, unknown>) => Promise<void>
}

export function ChannelGeneralTab({ instance, agents, readOnly = false, onUpdate }: ChannelGeneralTabProps) {
  const { t } = useTranslation('channels')

  const [displayName, setDisplayName] = useState(instance.display_name ?? '')
  const [agentId, setAgentId] = useState(instance.agent_id)
  const [enabled, setEnabled] = useState(instance.enabled)
  const [saving, setSaving] = useState(false)

  // Essential config fields (policies)
  const allConfigFields = configSchema[instance.channel_type] ?? []
  const essentialKeys = ESSENTIAL_CONFIG_KEYS[instance.channel_type] ?? ESSENTIAL_CONFIG_KEYS._default ?? []
  const essentialFields = allConfigFields.filter((f) => essentialKeys.includes(f.key))
  const existingConfig = (instance.config ?? {}) as Record<string, unknown>
  const initialPolicyValues = Object.fromEntries(
    essentialKeys.filter((k) => existingConfig[k] !== undefined).map((k) => [k, existingConfig[k]]),
  )
  const [policyValues, setPolicyValues] = useState<Record<string, unknown>>(initialPolicyValues)

  // Sync policy values when instance updates (e.g. after save)
  useEffect(() => {
    const config = (instance.config ?? {}) as Record<string, unknown>
    const keys = ESSENTIAL_CONFIG_KEYS[instance.channel_type] ?? ESSENTIAL_CONFIG_KEYS._default ?? []
    setPolicyValues(Object.fromEntries(keys.filter((k) => config[k] !== undefined).map((k) => [k, config[k]])))
    setDisplayName(instance.display_name ?? '')
    setAgentId(instance.agent_id)
    setEnabled(instance.enabled)
  }, [instance])

  const handlePolicyChange = useCallback((key: string, value: unknown) => {
    setPolicyValues((prev) => ({ ...prev, [key]: value }))
  }, [])

  const agentOptions = agents.map((a) => ({
    value: a.id,
    label: a.display_name || a.agent_key,
  }))

  const handleSave = async () => {
    setSaving(true)
    try {
      const cleanPolicies = Object.fromEntries(
        Object.entries(policyValues).filter(([, v]) => v !== undefined && v !== '' && v !== null),
      )
      const mergedConfig = { ...existingConfig, ...cleanPolicies }
      await onUpdate({
        display_name: displayName || null,
        agent_id: agentId,
        enabled,
        config: mergedConfig,
      })
    } catch {
      // toast shown by hook
    } finally {
      setSaving(false)
    }
  }

  return (
    <fieldset disabled={readOnly} className="m-0 flex min-w-0 flex-col gap-4 border-0 p-0">
      <section className="flex flex-col gap-4 rounded-lg border border-border-subtle bg-surface-base p-4">
        <h3 className="text-ui font-medium text-ink">{t('detail.general.identity')}</h3>

        <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <Field label={t('detail.general.name')} helper={t('detail.general.nameHint')}>
            <Input value={instance.name} readOnly className="font-mono" />
          </Field>
          <Field label={t('detail.general.channelType')}>
            <Input value={t(`channelTypes.${instance.channel_type}`, { defaultValue: instance.channel_type })} readOnly />
          </Field>
        </div>

        <Field label={t('detail.general.displayName')}>
          <Input
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder={t('detail.general.displayNamePlaceholder')}
          />
        </Field>

        <Field label={t('detail.general.agent')}>
          <Select
            value={agentId}
            onChange={(e) => setAgentId(e.target.value)}
            options={agentOptions}
            placeholder={t('detail.general.selectAgent')}
          />
        </Field>

        <div className="flex items-center gap-3">
          <Switch
            checked={enabled}
            onCheckedChange={setEnabled}
            aria-labelledby="channel-general-enabled"
          />
          <span id="channel-general-enabled" className="text-ui text-ink">{t('detail.general.enabled')}</span>
        </div>
      </section>

      {essentialFields.length > 0 && (
        <section className="flex flex-col gap-4 rounded-lg border border-border-subtle bg-surface-base p-4">
          <h3 className="text-ui font-medium text-ink">{t('detail.policies')}</h3>
          <ChannelFields
            fields={essentialFields}
            values={policyValues}
            onChange={handlePolicyChange}
            idPrefix="cg-pol"
            contextValues={policyValues}
          />
        </section>
      )}

      <div className="flex justify-end">
        <Button variant="primary" onClick={() => { void handleSave() }} loading={saving} disabled={readOnly}>
          {saving ? t('detail.general.saving') : t('detail.general.saveChanges')}
        </Button>
      </div>
    </fieldset>
  )
}

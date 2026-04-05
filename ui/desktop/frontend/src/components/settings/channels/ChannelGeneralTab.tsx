import { useState, useCallback, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Switch } from '../../common/Switch'
import { Combobox } from '../../common/Combobox'
import { ChannelFields } from './channel-field-renderer'
import { configSchema, ESSENTIAL_CONFIG_KEYS } from './channel-schemas'
import type { ChannelInstanceData } from '../../../types/channel'
import type { AgentData } from '../../../types/agent'

interface ChannelGeneralTabProps {
  instance: ChannelInstanceData
  agents: AgentData[]
  onUpdate: (updates: Record<string, unknown>) => Promise<void>
}

export function ChannelGeneralTab({ instance, agents, onUpdate }: ChannelGeneralTabProps) {
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
    <div className="space-y-4">
      {/* Identity section */}
      <section className="space-y-3 rounded-lg border border-border p-4">
        <h3 className="text-xs font-semibold text-text-secondary">{t('detail.general.identity')}</h3>

        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('detail.general.name')}</label>
          <div className="px-3 py-2 rounded-lg border border-border bg-surface-tertiary/50 text-xs text-text-muted font-mono">
            {instance.name}
          </div>
          <p className="text-[11px] text-text-muted">{t('detail.general.nameHint')}</p>
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('detail.general.channelType')}</label>
          <div className="px-3 py-2 rounded-lg border border-border bg-surface-tertiary/50 text-xs text-text-muted">
            {t(`channelTypes.${instance.channel_type}`)}
          </div>
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('detail.general.displayName')}</label>
          <input
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder={t('detail.general.displayNamePlaceholder')}
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>

        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('detail.general.agent')}</label>
          <Combobox value={agentId} onChange={setAgentId} options={agentOptions} placeholder={t('detail.general.selectAgent')} />
        </div>

        <div className="flex items-center gap-2">
          <Switch checked={enabled} onCheckedChange={setEnabled} />
          <span className="text-xs text-text-secondary">{t('detail.general.enabled')}</span>
        </div>
      </section>

      {/* Policies section */}
      {essentialFields.length > 0 && (
        <section className="space-y-3 rounded-lg border border-border p-4">
          <h3 className="text-xs font-semibold text-text-secondary">{t('detail.policies')}</h3>
          <ChannelFields
            fields={essentialFields}
            values={policyValues}
            onChange={handlePolicyChange}
            idPrefix="cg-pol"
            contextValues={policyValues}
          />
        </section>
      )}

      {/* Save button */}
      <button
        onClick={handleSave}
        disabled={saving}
        className="px-4 py-1.5 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-50 cursor-pointer"
      >
        {saving ? t('detail.general.saving') : t('detail.general.saveChanges')}
      </button>
    </div>
  )
}

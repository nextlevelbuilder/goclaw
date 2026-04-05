import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Combobox } from '../../common/Combobox'
import { Switch } from '../../common/Switch'
import { ChannelFields } from './channel-field-renderer'
import { credentialsSchema } from './channel-schemas'
import type { ChannelInstanceInput } from '../../../types/channel'
import type { AgentData } from '../../../types/agent'

interface ChannelFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  agents: AgentData[]
  telegramExists: boolean
  discordExists: boolean
  onSubmit: (input: ChannelInstanceInput) => Promise<unknown>
}

export function ChannelFormDialog({ open, onOpenChange, agents, telegramExists, discordExists, onSubmit }: ChannelFormDialogProps) {
  const { t } = useTranslation('channels')

  const [displayName, setDisplayName] = useState('')
  const [channelType, setChannelType] = useState('')
  const [agentId, setAgentId] = useState('')
  const [enabled, setEnabled] = useState(true)
  const [credentials, setCredentials] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  // Reset form when dialog opens
  useEffect(() => {
    if (!open) return
    setDisplayName('')
    setChannelType('')
    setAgentId('')
    setEnabled(true)
    setCredentials({})
    setError('')

    // Auto-select the only available type
    const available = []
    if (!telegramExists) available.push('telegram')
    if (!discordExists) available.push('discord')
    if (available.length === 1) setChannelType(available[0])
  }, [open, telegramExists, discordExists])

  const typeOptions = useMemo(() => {
    const opts = []
    if (!telegramExists) opts.push({ value: 'telegram', label: 'Telegram' })
    if (!discordExists) opts.push({ value: 'discord', label: 'Discord' })
    return opts
  }, [telegramExists, discordExists])

  const agentOptions = useMemo(
    () => agents.map((a) => ({ value: a.id, label: a.display_name || a.agent_key })),
    [agents],
  )

  const credFields = channelType ? (credentialsSchema[channelType] ?? []) : []

  const handleCredChange = (key: string, value: unknown) => {
    setCredentials((prev) => ({ ...prev, [key]: String(value ?? '') }))
  }

  const canCreate = !!channelType && !!agentId
    && credFields.filter((f) => f.required).every((f) => credentials[f.key]?.trim())

  const handleCreate = async () => {
    setLoading(true)
    setError('')
    try {
      await onSubmit({
        name: channelType, // auto-slug: "telegram" or "discord"
        displayName: displayName.trim(),
        channelType,
        agentId,
        credentials,
        config: {},
        enabled,
      })
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create channel')
    } finally {
      setLoading(false)
    }
  }

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm">
      <div className="bg-surface-secondary border border-border rounded-xl shadow-xl max-w-lg w-full mx-4 overflow-hidden flex flex-col" style={{ maxHeight: '85vh' }}>
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-5 py-4 shrink-0">
          <h3 className="text-sm font-semibold text-text-primary">{t('form.createTitle')}</h3>
          <button onClick={() => onOpenChange(false)} className="p-1 text-text-muted hover:text-text-primary transition-colors cursor-pointer">
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M18 6 6 18" /><path d="m6 6 12 12" />
            </svg>
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto overscroll-contain p-5 space-y-4">
          {/* Display Name */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.displayName')}</label>
            <input value={displayName} onChange={(e) => setDisplayName(e.target.value)} placeholder={t('form.displayNamePlaceholder')} className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent" />
          </div>

          {/* Channel Type */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.channelType')} *</label>
            {typeOptions.length === 1 ? (
              <div className="px-3 py-2 rounded-lg border border-border bg-surface-tertiary/50 text-sm text-text-muted">
                {typeOptions[0].label}
              </div>
            ) : (
              <Combobox
                value={channelType}
                onChange={(v) => { setChannelType(v); setCredentials({}) }}
                options={typeOptions}
                placeholder={t('form.selectType')}
                allowCustom={false}
              />
            )}
          </div>

          {/* Agent */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.agent')} *</label>
            <Combobox value={agentId} onChange={setAgentId} options={agentOptions} placeholder={t('form.selectAgent')} />
          </div>

          {/* Enabled */}
          <div className="flex items-center gap-2">
            <Switch checked={enabled} onCheckedChange={setEnabled} />
            <span className="text-xs text-text-secondary">{t('form.enabled')}</span>
          </div>

          {/* Credentials */}
          {credFields.length > 0 && (
            <div className="space-y-2 border-t border-border pt-4">
              <h4 className="text-xs font-semibold text-text-secondary">{t('form.credentials')}</h4>
              <ChannelFields fields={credFields} values={credentials} onChange={handleCredChange} idPrefix="cf-cred" />
            </div>
          )}
        </div>

        {/* Footer */}
        {error && <div className="px-5"><p className="text-xs text-error">{error}</p></div>}
        <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-4 shrink-0">
          <button onClick={() => onOpenChange(false)} className="px-3 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors cursor-pointer">
            {t('form.cancel')}
          </button>
          <button onClick={handleCreate} disabled={!canCreate || loading} className="px-4 py-1.5 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-50 cursor-pointer">
            {loading ? t('form.saving') : t('form.create')}
          </button>
        </div>
      </div>
    </div>
  )
}

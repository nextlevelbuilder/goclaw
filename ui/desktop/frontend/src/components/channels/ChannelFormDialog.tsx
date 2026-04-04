import { useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { Combobox } from '../common/Combobox'
import { Switch } from '../common/Switch'
import { ChannelFields } from './channel-field-renderer'
import { credentialsSchema } from './channel-schemas'
import { channelFormSchema, type ChannelFormData } from '../../schemas/channel.schema'
import type { ChannelInstanceInput } from '../../types/channel'
import type { AgentData } from '../../types/agent'

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

  const { handleSubmit, watch, setValue, reset, formState: { errors, isSubmitting } } = useForm<ChannelFormData>({
    resolver: zodResolver(channelFormSchema),
    mode: 'onChange',
  })

  const channelType = watch('channelType')
  const credentials = watch('credentials')

  // Reset form when dialog opens
  useEffect(() => {
    if (!open) return
    const available = []
    if (!telegramExists) available.push('telegram')
    if (!discordExists) available.push('discord')
    reset({
      displayName: '',
      channelType: available.length === 1 ? available[0] : '',
      agentId: '',
      enabled: true,
      credentials: {},
    })
  }, [open, telegramExists, discordExists, reset])

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
    setValue('credentials', { ...credentials, [key]: String(value ?? '') })
  }

  const canCreate = !!channelType && !!watch('agentId')
    && credFields.filter((f) => f.required).every((f) => credentials?.[f.key]?.trim())

  const onValid = async (data: ChannelFormData) => {
    await onSubmit({
      name: data.channelType,
      displayName: data.displayName,
      channelType: data.channelType,
      agentId: data.agentId,
      credentials: data.credentials,
      config: {},
      enabled: data.enabled,
    })
    onOpenChange(false)
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
            <input value={watch('displayName')} onChange={(e) => setValue('displayName', e.target.value)} placeholder={t('form.displayNamePlaceholder')} className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent" />
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
                onChange={(v) => { setValue('channelType', v, { shouldValidate: true }); setValue('credentials', {}) }}
                options={typeOptions}
                placeholder={t('form.selectType')}
                allowCustom={false}
              />
            )}
            {errors.channelType && <p className="text-xs text-error">{errors.channelType.message}</p>}
          </div>

          {/* Agent */}
          <div className="space-y-1">
            <label className="text-xs font-medium text-text-secondary">{t('form.agent')} *</label>
            <Combobox value={watch('agentId')} onChange={(v) => setValue('agentId', v, { shouldValidate: true })} options={agentOptions} placeholder={t('form.selectAgent')} />
            {errors.agentId && <p className="text-xs text-error">{errors.agentId.message}</p>}
          </div>

          {/* Enabled */}
          <div className="flex items-center gap-2">
            <Switch checked={watch('enabled')} onCheckedChange={(v) => setValue('enabled', v)} />
            <span className="text-xs text-text-secondary">{t('form.enabled')}</span>
          </div>

          {/* Credentials */}
          {credFields.length > 0 && (
            <div className="space-y-2 border-t border-border pt-4">
              <h4 className="text-xs font-semibold text-text-secondary">{t('form.credentials')}</h4>
              <ChannelFields fields={credFields} values={credentials ?? {}} onChange={handleCredChange} idPrefix="cf-cred" />
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-4 shrink-0">
          <button onClick={() => onOpenChange(false)} className="px-3 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors cursor-pointer">
            {t('form.cancel')}
          </button>
          <button onClick={handleSubmit(onValid)} disabled={!canCreate || isSubmitting} className="px-4 py-1.5 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-50 cursor-pointer">
            {isSubmitting ? t('form.saving') : t('form.create')}
          </button>
        </div>
      </div>
    </div>
  )
}

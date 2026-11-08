import { useEffect, useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { Button, Dialog, DialogFooter, Field, Input, Select, Switch } from '../ui'
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
  /** Disables the form while the gateway is unreachable. */
  readOnly?: boolean
  onSubmit: (input: ChannelInstanceInput) => Promise<unknown>
}

export function ChannelFormDialog({ open, onOpenChange, agents, telegramExists, discordExists, readOnly = false, onSubmit }: ChannelFormDialogProps) {
  const { t } = useTranslation('channels')
  const [attempted, setAttempted] = useState(false)

  const { handleSubmit, watch, setValue, reset, formState: { isSubmitting } } = useForm<ChannelFormData>({
    resolver: zodResolver(channelFormSchema),
    mode: 'onChange',
    defaultValues: { displayName: '', channelType: '', agentId: '', enabled: true, credentials: {} },
  })

  const channelType = watch('channelType')
  const agentId = watch('agentId')
  const credentials = watch('credentials')

  // Reset form when dialog opens
  useEffect(() => {
    if (!open) return
    setAttempted(false)
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
    if (!telegramExists) opts.push({ value: 'telegram', label: t('channelTypes.telegram') })
    if (!discordExists) opts.push({ value: 'discord', label: t('channelTypes.discord') })
    return opts
  }, [telegramExists, discordExists, t])

  const agentOptions = useMemo(
    () => agents.map((a) => ({ value: a.id, label: a.display_name || a.agent_key })),
    [agents],
  )

  const credFields = channelType ? (credentialsSchema[channelType] ?? []) : []

  const handleCredChange = (key: string, value: unknown) => {
    setValue('credentials', { ...credentials, [key]: String(value ?? '') })
  }

  const missingCredential = credFields.filter((f) => f.required).some((f) => !credentials?.[f.key]?.trim())
  const canCreate = !!channelType && !!agentId && !missingCredential

  const onValid = async (data: ChannelFormData) => {
    try {
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
    } catch {
      // toast shown by hook
    }
  }

  const submit = () => {
    setAttempted(true)
    if (!canCreate) return
    void handleSubmit(onValid)()
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('form.createTitle')}
      dismissible={!isSubmitting}
      footer={
        <DialogFooter>
          <Button onClick={() => onOpenChange(false)} disabled={isSubmitting}>{t('form.cancel')}</Button>
          <Button variant="primary" onClick={submit} loading={isSubmitting} disabled={readOnly || (attempted && !canCreate)}>
            {isSubmitting ? t('form.saving') : t('form.create')}
          </Button>
        </DialogFooter>
      }
    >
      <fieldset disabled={readOnly} className="m-0 flex min-w-0 flex-col gap-4 border-0 p-0">
        <Field label={t('form.displayName')}>
          <Input
            value={watch('displayName')}
            onChange={(e) => setValue('displayName', e.target.value)}
            placeholder={t('form.displayNamePlaceholder')}
          />
        </Field>

        <Field label={t('form.channelType')} required error={attempted && !channelType ? t('form.errors.typeRequired') : undefined}>
          <Select
            value={channelType}
            onChange={(e) => { setValue('channelType', e.target.value, { shouldValidate: true }); setValue('credentials', {}) }}
            options={typeOptions}
            placeholder={t('form.selectType')}
          />
        </Field>

        <Field label={t('form.agent')} required error={attempted && !agentId ? t('form.errors.agentRequired') : undefined}>
          <Select
            value={agentId}
            onChange={(e) => setValue('agentId', e.target.value, { shouldValidate: true })}
            options={agentOptions}
            placeholder={t('form.selectAgent')}
          />
        </Field>

        <div className="flex items-center gap-3">
          <Switch checked={watch('enabled')} onCheckedChange={(v) => setValue('enabled', v)} aria-labelledby="channel-form-enabled" />
          <span id="channel-form-enabled" className="text-ui text-ink">{t('form.enabled')}</span>
        </div>

        {credFields.length > 0 && (
          <div className="flex flex-col gap-3 border-t border-border-subtle pt-4">
            <h4 className="text-ui font-medium text-ink">{t('form.credentials')}</h4>
            <ChannelFields fields={credFields} values={credentials ?? {}} onChange={handleCredChange} idPrefix="cf-cred" />
            {attempted && missingCredential && (
              <p role="alert" className="text-caption text-danger">{t('form.errors.tokenRequired')}</p>
            )}
          </div>
        )}
      </fieldset>
    </Dialog>
  )
}

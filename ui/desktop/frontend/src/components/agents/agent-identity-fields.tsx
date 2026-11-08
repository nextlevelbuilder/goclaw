import { useTranslation } from 'react-i18next'
import { Combobox } from '../common/Combobox'
import { CatalogueNotice } from './CatalogueNotice'
import type { UseFormRegister, UseFormWatch, UseFormSetValue, FieldErrors } from 'react-hook-form'
import type { AgentFormData } from '../../schemas/agent.schema'
import { Field, Input, Switch } from '../ui'

interface AgentIdentityFieldsProps {
  isEditing: boolean
  register: UseFormRegister<AgentFormData>
  watch: UseFormWatch<AgentFormData>
  setValue: UseFormSetValue<AgentFormData>
  errors: FieldErrors<AgentFormData>
  providerOptions: { value: string; label: string }[]
  modelOptions: { value: string; label: string }[]
  modelsLoading: boolean
  catalogueStale: boolean
  catalogueError: string | null
  onRetryCatalogue: () => void
  verifyResult: { valid: boolean; error?: string } | null
  onAgentKeyChange: (val: string) => void
}

export function AgentIdentityFields({
  isEditing, register, watch, setValue, errors,
  providerOptions, modelOptions, modelsLoading,
  catalogueStale, catalogueError, onRetryCatalogue,
  verifyResult,
  onAgentKeyChange,
}: AgentIdentityFieldsProps) {
  const { t } = useTranslation(['agents', 'desktop', 'common'])
  const agentKey = watch('agentKey')
  const providerName = watch('providerName')
  const model = watch('model')

  return (
    <div className="flex flex-col gap-4">
      <div className="flex gap-3">
        <Field label={t('agents:create.displayName').replace(' *', '')} required error={errors.displayName ? t('agents:screen.errors.displayNameRequired') : undefined} className="flex-1">
          <Input {...register('displayName')} placeholder={t('agents:create.displayNamePlaceholder')} />
        </Field>
        <Field label={t('agents:identity.emoji')} className="w-20">
          <Input value={watch('emoji') ?? ''} onChange={(e) => setValue('emoji', e.target.value.slice(0, 2))} maxLength={2} className="text-center" />
        </Field>
      </div>

      {!isEditing && (
        <Field label={t('agents:create.agentKey').replace(' *', '')} helper={t('agents:create.agentKeyHint')} error={errors.agentKey?.message}>
          <Input value={agentKey} onChange={(e) => onAgentKeyChange(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, '-'))} className="font-mono" />
        </Field>
      )}

      <Field label={t('common:provider')} required error={errors.providerName ? t('agents:screen.errors.providerRequired') : undefined}>
        <Combobox value={providerName} onChange={(v) => setValue('providerName', v, { shouldValidate: true })} options={providerOptions} placeholder={t('agents:create.selectProvider')} />
      </Field>

      <Field
        label={t('common:model')}
        required
        error={verifyResult && !verifyResult.valid ? (verifyResult.error || t('desktop:agent.verifyFailed')) : undefined}
        helper={verifyResult?.valid && !isEditing ? t('desktop:agent.verified') : undefined}
      >
        <Combobox value={model} onChange={(v) => setValue('model', v, { shouldValidate: true })} options={modelOptions} placeholder={modelsLoading ? t('agents:create.loadingModels') : t('agents:create.enterOrSelectModel')} loading={modelsLoading} allowCustom />
      </Field>
      <CatalogueNotice stale={catalogueStale} error={catalogueError} onRetry={onRetryCatalogue} />

      <label className="flex items-center gap-2 text-ui text-ink">
        <Switch checked={watch('isDefault')} onCheckedChange={(v) => setValue('isDefault', v)} aria-label={t('agents:identity.defaultAgent')} />
        <span>{t('agents:identity.defaultAgent')}</span>
      </label>
    </div>
  )
}

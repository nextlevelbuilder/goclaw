import { useTranslation } from 'react-i18next'
import { Combobox } from '../common/Combobox'
import { Switch } from '../common/Switch'
import type { UseFormRegister, UseFormWatch, UseFormSetValue, FieldErrors } from 'react-hook-form'
import type { AgentFormData } from '../../schemas/agent.schema'

interface AgentIdentityFieldsProps {
  isEditing: boolean
  register: UseFormRegister<AgentFormData>
  watch: UseFormWatch<AgentFormData>
  setValue: UseFormSetValue<AgentFormData>
  errors: FieldErrors<AgentFormData>
  providerOptions: { value: string; label: string }[]
  modelOptions: { value: string; label: string }[]
  modelsLoading: boolean
  verifyResult: { valid: boolean; error?: string } | null
  onAgentKeyChange: (val: string) => void
}

export function AgentIdentityFields({
  isEditing, register, watch, setValue, errors,
  providerOptions, modelOptions, modelsLoading, verifyResult,
  onAgentKeyChange,
}: AgentIdentityFieldsProps) {
  const { t } = useTranslation(['agents', 'desktop', 'common'])
  const agentKey = watch('agentKey')
  const providerName = watch('providerName')
  const model = watch('model')

  return (
    <div className="space-y-4">
      {/* Display name + emoji */}
      <div className="flex gap-2">
        <div className="space-y-1 flex-1">
          <label className="text-xs font-medium text-text-secondary">{t('agents:create.displayName').replace(' *', '')}</label>
          <input {...register('displayName')} placeholder={t('agents:create.displayNamePlaceholder')} className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent" />
        </div>
        <div className="space-y-1 w-16">
          <label className="text-xs font-medium text-text-secondary">{t('agents:identity.emoji')}</label>
          <input value={watch('emoji') ?? ''} onChange={(e) => setValue('emoji', e.target.value.slice(0, 2))} maxLength={2} className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary text-center focus:outline-none focus:ring-1 focus:ring-accent" />
        </div>
      </div>

      {/* Agent key (create only) */}
      {!isEditing && (
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">Agent Key</label>
          <input value={agentKey} onChange={(e) => onAgentKeyChange(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, '-'))} className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary font-mono focus:outline-none focus:ring-1 focus:ring-accent" />
          {errors.agentKey && <p className="text-xs text-error">{errors.agentKey.message}</p>}
        </div>
      )}

      {/* Provider */}
      <div className="space-y-1">
        <label className="text-xs font-medium text-text-secondary">{t('common:provider')}</label>
        <Combobox value={providerName} onChange={(v) => setValue('providerName', v, { shouldValidate: true })} options={providerOptions} placeholder={t('agents:create.selectProvider')} />
      </div>

      {/* Model */}
      <div className="space-y-1">
        <label className="text-xs font-medium text-text-secondary">{t('common:model')}</label>
        <Combobox value={model} onChange={(v) => setValue('model', v, { shouldValidate: true })} options={modelOptions} placeholder={modelsLoading ? t('agents:create.loadingModels') : t('agents:create.enterOrSelectModel')} allowCustom />
        {verifyResult && !verifyResult.valid && <p className="text-xs text-error">{verifyResult.error || t('desktop:agent.verifyFailed')}</p>}
        {verifyResult?.valid && !isEditing && <p className="text-xs text-success">{t('desktop:agent.verified')}</p>}
      </div>

      {/* Default toggle */}
      <div className="flex items-center gap-2">
        <Switch checked={watch('isDefault')} onCheckedChange={(v) => setValue('isDefault', v)} />
        <span className="text-xs text-text-secondary">{t('agents:identity.defaultAgent')}</span>
      </div>
    </div>
  )
}

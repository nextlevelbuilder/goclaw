import { useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { Combobox } from '../common/Combobox'
import { Switch } from '../common/Switch'
import { PROVIDER_TYPES } from '../../constants/providers'
import { slugify } from '../../lib/slug'
import { providerFormSchema, type ProviderFormData } from '../../schemas/provider.schema'
import type { ProviderData, ProviderInput } from '../../types/provider'

interface ProviderFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  provider?: ProviderData | null
  onSubmit: (input: ProviderInput) => Promise<void>
}

export function ProviderFormDialog({ open, onOpenChange, provider, onSubmit }: ProviderFormDialogProps) {
  const isEditing = !!provider
  const { t } = useTranslation(['providers', 'common'])

  const { register, handleSubmit, watch, setValue, reset, formState: { errors, isSubmitting } } = useForm<ProviderFormData>({
    resolver: zodResolver(providerFormSchema),
    mode: 'onChange',
  })

  // Reset form when dialog opens
  useEffect(() => {
    if (open) {
      reset({
        providerType: provider?.provider_type ?? '',
        displayName: provider?.display_name ?? '',
        apiBase: provider?.api_base ?? '',
        apiKey: '',
        enabled: provider?.enabled ?? true,
      })
    }
  }, [open, provider, reset])

  const providerType = watch('providerType')
  const displayName = watch('displayName')
  const apiBase = watch('apiBase')

  // Auto-set api_base from provider type
  const typeInfo = useMemo(() => PROVIDER_TYPES.find((t) => t.value === providerType), [providerType])
  useEffect(() => {
    if (!isEditing && typeInfo?.apiBase && !apiBase) {
      setValue('apiBase', typeInfo.apiBase)
    }
  }, [typeInfo, isEditing, apiBase, setValue])

  const name = useMemo(() => {
    if (isEditing) return provider!.name
    return slugify(displayName || providerType) || 'provider'
  }, [isEditing, provider, displayName, providerType])

  const onValid = async (data: ProviderFormData) => {
    const input: ProviderInput = {
      name,
      display_name: data.displayName || undefined,
      provider_type: data.providerType,
      api_base: data.apiBase || undefined,
      enabled: data.enabled,
    }
    if (data.apiKey) input.api_key = data.apiKey
    await onSubmit(input)
    onOpenChange(false)
  }

  if (!open) return null

  const typeOptions = PROVIDER_TYPES.map((t) => ({ value: t.value, label: t.label }))

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm">
      <div className="bg-surface-secondary border border-border rounded-xl shadow-xl max-w-md w-full mx-4 p-5 space-y-4">
        <h3 className="text-sm font-semibold text-text-primary">
          {isEditing ? t('providers:form.editTitle') : t('providers:form.createTitle')}
        </h3>

        {/* Provider type */}
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('providers:form.providerType').replace(' *', '')}</label>
          <Combobox
            value={providerType}
            onChange={(v) => setValue('providerType', v, { shouldValidate: true })}
            options={typeOptions}
            placeholder={t('common:selectProvider')}
            disabled={isEditing}
          />
          {errors.providerType && <p className="text-xs text-error">{errors.providerType.message}</p>}
        </div>

        {/* Display name */}
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('providers:form.displayName')}</label>
          <input
            {...register('displayName')}
            placeholder={typeInfo?.label ?? 'My Provider'}
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>

        {/* API Base */}
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('providers:form.apiBase')}</label>
          <input
            {...register('apiBase')}
            placeholder="https://api.example.com"
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>

        {/* API Key */}
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">
            {t('providers:form.apiKey')} {isEditing && <span className="text-text-muted font-normal">({t('providers:form.apiKeyEditPlaceholder').toLowerCase()})</span>}
          </label>
          <input
            type="password"
            {...register('apiKey')}
            placeholder={isEditing ? '••••••••' : 'sk-...'}
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>

        {/* Enabled toggle */}
        <div className="flex items-center gap-2">
          <Switch checked={watch('enabled')} onCheckedChange={(v) => setValue('enabled', v)} />
          <span className="text-xs text-text-secondary">{t('providers:form.enabled')}</span>
        </div>

        <div className="flex justify-end gap-2 pt-1">
          <button
            onClick={() => onOpenChange(false)}
            className="px-3 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors"
          >
            {t('providers:form.cancel')}
          </button>
          <button
            onClick={handleSubmit(onValid)}
            disabled={isSubmitting || !providerType}
            className="px-4 py-1.5 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-50"
          >
            {isSubmitting ? '...' : isEditing ? t('providers:form.save') : t('providers:form.create')}
          </button>
        </div>
      </div>
    </div>
  )
}

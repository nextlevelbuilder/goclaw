import { useState, useEffect, useMemo, useCallback } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { useProviders } from '../../hooks/use-providers'
import { getApiClient } from '../../lib/api'
import { slugify } from '../../lib/slug'
import { AgentPresetSelector } from './agent-preset-selector'
import { AgentIdentityFields } from './agent-identity-fields'
import { agentFormSchema, type AgentFormData } from '../../schemas/agent.schema'
import type { AgentData, AgentInput } from '../../types/agent'

interface AgentFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  agent?: AgentData | null
  onSubmit: (input: AgentInput) => Promise<AgentData | void>
}

export function AgentFormDialog({ open, onOpenChange, agent, onSubmit }: AgentFormDialogProps) {
  const isEditing = !!agent
  const { t } = useTranslation(['agents', 'desktop', 'common'])
  const { providers } = useProviders()

  const { register, handleSubmit, watch, setValue, reset, formState: { errors, isSubmitting } } = useForm<AgentFormData>({
    resolver: zodResolver(agentFormSchema),
    mode: 'onChange',
  })

  // UI-only state (not form data)
  const [models, setModels] = useState<string[]>([])
  const [modelsLoading, setModelsLoading] = useState(false)
  const [verifyResult, setVerifyResult] = useState<{ valid: boolean; error?: string } | null>(null)
  const [verifying, setVerifying] = useState(false)
  const [submitError, setSubmitError] = useState('')
  const [selectedPresetKey, setSelectedPresetKey] = useState('')

  // Reset form when dialog opens
  useEffect(() => {
    if (!open) return
    reset({
      displayName: agent?.display_name ?? '',
      emoji: (agent?.other_config?.emoji as string) ?? '🦊',
      agentKey: agent?.agent_key ?? '',
      providerName: agent?.provider ?? '',
      model: agent?.model ?? '',
      description: (agent?.other_config?.description as string) ?? '',
      isDefault: agent?.is_default ?? false,
    })
    setSubmitError('')
    setSelectedPresetKey('')
    setVerifyResult(isEditing ? { valid: true } : null)
    setModels([])
  }, [open, agent, isEditing, reset])

  const providerName = watch('providerName')
  const model = watch('model')
  const displayName = watch('displayName')

  // Derive agentKey from displayName/preset when creating
  useEffect(() => {
    if (isEditing) return
    const derived = selectedPresetKey || slugify(displayName) || 'agent'
    setValue('agentKey', derived)
  }, [displayName, selectedPresetKey, isEditing, setValue])

  const selectedProvider = useMemo(() => providers.find((p) => p.name === providerName), [providers, providerName])

  // Load models when provider changes
  const loadModels = useCallback(async (providerId: string) => {
    setModelsLoading(true)
    try {
      const res = await getApiClient().get<{ models: Array<{ id: string }> }>(`/v1/providers/${providerId}/models`)
      setModels((res.models ?? []).map((m) => m.id))
    } catch { setModels([]) } finally { setModelsLoading(false) }
  }, [])

  useEffect(() => { if (selectedProvider?.id) loadModels(selectedProvider.id) }, [selectedProvider?.id, loadModels])
  useEffect(() => { if (!isEditing) setVerifyResult(null) }, [providerName, model, isEditing])

  const providerOptions = useMemo(() => providers.filter((p) => p.enabled).map((p) => ({ value: p.name, label: p.display_name || p.name })), [providers])
  const modelOptions = useMemo(() => models.map((m) => ({ value: m, label: m })), [models])

  const handleVerify = async () => {
    if (!selectedProvider?.id || !model.trim()) return
    setVerifying(true)
    try {
      const res = await getApiClient().post<{ valid: boolean; error?: string }>(`/v1/providers/${selectedProvider.id}/verify`, { model: model.trim() })
      setVerifyResult({ valid: res.valid, error: res.error })
    } catch (err) {
      setVerifyResult({ valid: false, error: err instanceof Error ? err.message : 'Verification failed' })
    } finally { setVerifying(false) }
  }

  const onValid = async (data: AgentFormData) => {
    setSubmitError('')
    try {
      const otherConfig: Record<string, unknown> = {}
      if (data.description?.trim()) otherConfig.description = data.description.trim()
      if (data.emoji?.trim()) otherConfig.emoji = data.emoji.trim()
      await onSubmit({
        agent_key: data.agentKey,
        display_name: data.displayName.trim() || undefined,
        provider: data.providerName,
        model: data.model.trim(),
        agent_type: isEditing ? agent!.agent_type : 'predefined',
        is_default: data.isDefault || undefined,
        other_config: Object.keys(otherConfig).length > 0 ? otherConfig : undefined,
      })
      onOpenChange(false)
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : 'Failed to save agent')
    }
  }

  if (!open) return null

  const canCreate = isEditing
    ? !!providerName && !!model.trim()
    : !!displayName.trim() && !!providerName && !!model.trim() && verifyResult?.valid && !!watch('description')?.trim()

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 backdrop-blur-sm">
      <div className="bg-surface-secondary border border-border rounded-xl shadow-xl max-w-3xl w-full mx-4 overflow-hidden flex flex-col" style={{ maxHeight: '85vh' }}>
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-5 py-4 shrink-0">
          <h3 className="text-sm font-semibold text-text-primary">
            {isEditing ? t('agents:detail.tabs.agent') + ' — ' + t('common:edit') : t('agents:create.title')}
          </h3>
          <button onClick={() => onOpenChange(false)} className="p-1 text-text-muted hover:text-text-primary transition-colors">
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M18 6 6 18" /><path d="m6 6 12 12" />
            </svg>
          </button>
        </div>

        {/* Body — 2 columns */}
        <div className="flex-1 overflow-y-auto overscroll-contain p-5">
          <div className="grid grid-cols-2 gap-5">
            {/* Left column — identity & model */}
            <AgentIdentityFields
              isEditing={isEditing}
              register={register}
              watch={watch}
              setValue={setValue}
              errors={errors}
              providerOptions={providerOptions}
              modelOptions={modelOptions}
              modelsLoading={modelsLoading}
              verifyResult={verifyResult}
              onAgentKeyChange={(val) => { setSelectedPresetKey(''); setValue('agentKey', val, { shouldValidate: true }) }}
            />

            {/* Right column — personality */}
            <div className="space-y-1.5 flex flex-col">
              <label className="text-xs font-medium text-text-secondary">{t('agents:detail.personality')}</label>
              {!isEditing && (
                <AgentPresetSelector
                  currentDescription={watch('description') ?? ''}
                  onSelect={({ description, emoji, displayName, agentKey }) => {
                    setValue('description', description)
                    setValue('emoji', emoji)
                    setValue('displayName', displayName)
                    setSelectedPresetKey(agentKey)
                  }}
                />
              )}
              <textarea {...register('description')} placeholder={t('agents:create.descriptionPlaceholder')} className="flex-1 min-h-[200px] w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent resize-y" />
              {errors.description && <p className="text-xs text-error">{errors.description.message}</p>}
            </div>
          </div>
        </div>

        {submitError && <div className="px-5"><p className="text-xs text-error">{submitError}</p></div>}

        {/* Footer */}
        <div className="flex items-center justify-between border-t border-border px-5 py-4 shrink-0">
          <div>{verifyResult && !verifyResult.valid && <span className="text-[11px] text-error">{verifyResult.error}</span>}</div>
          <div className="flex items-center gap-2">
            <button onClick={() => onOpenChange(false)} className="px-3 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors">{t('agents:create.cancel')}</button>
            {!isEditing && selectedProvider?.id && model.trim() && !verifyResult?.valid && (
              <button onClick={handleVerify} disabled={verifying} className="border border-border rounded-lg px-3 py-1.5 text-xs text-text-secondary hover:bg-surface-tertiary transition-colors disabled:opacity-50 flex items-center gap-1.5">
                {verifying ? (<><svg className="h-3.5 w-3.5 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}><path d="M21 12a9 9 0 1 1-6.219-8.56" /></svg>{t('desktop:agent.verifying')}</>) : t('desktop:agent.verifyModel')}
              </button>
            )}
            <button onClick={handleSubmit(onValid)} disabled={!canCreate || isSubmitting} className="px-4 py-1.5 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-50 flex items-center gap-1.5">
              {isSubmitting ? '...' : isEditing ? t('common:save') : (<>{verifyResult?.valid && (<svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"><path d="M20 6 9 17l-5-5" /></svg>)}{t('desktop:agent.summon')}</>)}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

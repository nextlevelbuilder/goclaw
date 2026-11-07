import { useState, useEffect, useMemo } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { useProviderCapabilities } from '../../hooks/use-provider-capabilities'
import { isCatalogueStale, modelOptionsFor, providerOptionsFrom } from '../../api/provider-capabilities'
import { getApiClient } from '../../lib/api'
import { slugify } from '../../lib/slug'
import { Button, Dialog, DialogFooter, Field, Textarea } from '../ui'
import { IconCheck } from '../common/Icons'
import { AGENT_PRESETS, AgentPresetSelector, resolvePreset } from './agent-preset-selector'
import { AgentIdentityFields } from './agent-identity-fields'
import { agentFormSchema, type AgentFormData } from '../../schemas/agent.schema'
import type { AgentData, AgentInput } from '../../types/agent'

interface AgentFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  agent?: AgentData | null
  /** Key of a preset (AGENT_PRESETS) to fill the form with when the dialog opens. */
  initialPresetKey?: string | null
  onSubmit: (input: AgentInput) => Promise<AgentData | void>
}

export function AgentFormDialog({ open, onOpenChange, agent, initialPresetKey, onSubmit }: AgentFormDialogProps) {
  const isEditing = !!agent
  const { t } = useTranslation(['agents', 'desktop', 'common'])
  const { capabilities, isLoading: catalogueLoading, error: catalogueError, refetch: refetchCatalogue } = useProviderCapabilities()

  const { register, handleSubmit, watch, setValue, reset, formState: { errors, isSubmitting } } = useForm<AgentFormData>({
    resolver: zodResolver(agentFormSchema),
    mode: 'onChange',
    defaultValues: { displayName: '', emoji: '🦊', agentKey: '', providerName: '', model: '', description: '', isDefault: false },
  })

  const [verifyResult, setVerifyResult] = useState<{ valid: boolean; error?: string } | null>(null)
  const [verifying, setVerifying] = useState(false)
  const [submitError, setSubmitError] = useState('')
  const [selectedPresetKey, setSelectedPresetKey] = useState('')

  // Reset (and apply the starting template) each time the dialog opens
  useEffect(() => {
    if (!open) return
    const preset = !agent && initialPresetKey ? AGENT_PRESETS.find((p) => p.key === initialPresetKey) : undefined
    const resolved = preset ? resolvePreset(t, preset) : null
    reset({
      displayName: resolved?.displayName ?? agent?.display_name ?? '',
      emoji: resolved?.emoji ?? agent?.emoji ?? (agent?.other_config?.emoji as string) ?? '🦊',
      agentKey: resolved?.agentKey ?? agent?.agent_key ?? '',
      providerName: agent?.provider ?? '',
      model: agent?.model ?? '',
      description: resolved?.description ?? agent?.agent_description ?? (agent?.other_config?.description as string) ?? '',
      isDefault: agent?.is_default ?? false,
    })
    setSubmitError('')
    setSelectedPresetKey(resolved?.agentKey ?? '')
    setVerifyResult(isEditing ? { valid: true } : null)
    // t is stable for the open dialog; reset must only run when it opens
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, agent, isEditing, initialPresetKey, reset])

  const providerName = watch('providerName')
  const model = watch('model')
  const displayName = watch('displayName')

  // Derive agentKey from displayName/preset when creating
  useEffect(() => {
    if (isEditing) return
    const derived = selectedPresetKey || slugify(displayName) || 'agent'
    setValue('agentKey', derived)
  }, [displayName, selectedPresetKey, isEditing, setValue])

  // Provider identity in agent configs is the provider name (`capabilities[].id`).
  const capability = useMemo(() => capabilities.find((c) => c.id === providerName), [capabilities, providerName])
  const providerRef = capability?.provider_id ?? capability?.id

  useEffect(() => { if (!isEditing) setVerifyResult(null) }, [providerName, model, isEditing])

  const providerOptions = useMemo(() => providerOptionsFrom(capabilities), [capabilities])
  const modelOptions = useMemo(() => modelOptionsFor(capability), [capability])
  const catalogueStale = isCatalogueStale(capability)

  const handleVerify = async () => {
    if (!providerRef || !model.trim()) return
    setVerifying(true)
    try {
      const res = await getApiClient().post<{ valid: boolean; error?: string }>(`/v1/providers/${providerRef}/verify`, { model: model.trim() })
      setVerifyResult({ valid: res.valid, error: res.error })
    } catch (err) {
      setVerifyResult({ valid: false, error: err instanceof Error ? err.message : t('desktop:agent.verifyFailed') })
    } finally { setVerifying(false) }
  }

  const onValid = async (data: AgentFormData) => {
    setSubmitError('')
    try {
      await onSubmit({
        agent_key: data.agentKey,
        display_name: data.displayName.trim() || undefined,
        provider: data.providerName,
        model: data.model.trim(),
        agent_type: isEditing ? agent!.agent_type : 'predefined',
        is_default: data.isDefault || undefined,
        emoji: data.emoji?.trim() || null,
        agent_description: data.description?.trim() || null,
      })
      onOpenChange(false)
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : t('agents:create.failedToCreate'))
    }
  }

  const canCreate = isEditing
    ? !!providerName && !!model.trim()
    : !!displayName.trim() && !!providerName && !!model.trim() && verifyResult?.valid && !!watch('description')?.trim()

  const showVerify = !isEditing && providerRef && model.trim() && !verifyResult?.valid

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="xl"
      title={isEditing ? `${t('agents:detail.tabs.agent')} — ${t('common:edit')}` : t('agents:create.title')}
      dismissible={!isSubmitting}
      footer={(
        <DialogFooter>
          {submitError && <p role="alert" className="mr-auto text-caption text-danger">{submitError}</p>}
          <Button variant="secondary" onClick={() => onOpenChange(false)} disabled={isSubmitting}>{t('agents:create.cancel')}</Button>
          {showVerify && (
            <Button variant="secondary" onClick={handleVerify} loading={verifying}>
              {verifying ? t('desktop:agent.verifying') : t('desktop:agent.verifyModel')}
            </Button>
          )}
          <Button
            variant="primary"
            onClick={handleSubmit(onValid)}
            disabled={!canCreate}
            loading={isSubmitting}
            icon={!isEditing && verifyResult?.valid ? <IconCheck size={14} /> : undefined}
          >
            {isEditing ? t('common:save') : t('desktop:agent.summon')}
          </Button>
        </DialogFooter>
      )}
    >
      <div className="grid grid-cols-1 gap-5 md:grid-cols-2">
        <AgentIdentityFields
          isEditing={isEditing}
          register={register}
          watch={watch}
          setValue={setValue}
          errors={errors}
          providerOptions={providerOptions}
          modelOptions={modelOptions}
          modelsLoading={catalogueLoading}
          catalogueStale={catalogueStale}
          catalogueError={catalogueError}
          onRetryCatalogue={() => { void refetchCatalogue() }}
          verifyResult={verifyResult}
          onAgentKeyChange={(val) => { setSelectedPresetKey(''); setValue('agentKey', val, { shouldValidate: true }) }}
        />

        <div className="flex flex-col gap-2">
          {!isEditing && (
            <AgentPresetSelector
              currentDescription={watch('description') ?? ''}
              onSelect={({ description, emoji, displayName: presetName, agentKey }) => {
                setValue('description', description, { shouldValidate: true })
                setValue('emoji', emoji)
                setValue('displayName', presetName, { shouldValidate: true })
                setSelectedPresetKey(agentKey)
              }}
            />
          )}
          <Field
            label={t('agents:detail.personality')}
            helper={t('agents:create.descriptionHint')}
            error={errors.description ? t('agents:screen.errors.descriptionRequired') : undefined}
            className="flex-1"
          >
            <Textarea {...register('description')} placeholder={t('agents:create.descriptionPlaceholder')} className="min-h-[200px] flex-1" />
          </Field>
        </div>
      </div>
    </Dialog>
  )
}

import { useEffect, useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { Button, Dialog, DialogFooter, Field, Input, Select, Switch } from '../ui'
import { IconAlertTriangle, IconCheckCircle, IconXCircle } from '../common/Icons'
import { ProviderMetaChips } from './ProviderMetaChips'
import { PROVIDER_TYPES } from '../../constants/providers'
import { slugify } from '../../lib/slug'
import { providerService, type ProviderVerifyResult } from '../../services/provider-service'
import { providerFormSchema, type ProviderFormData } from '../../schemas/provider.schema'
import type { ProviderData, ProviderInput } from '../../types/provider'

interface ProviderFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  provider?: ProviderData | null
  onSubmit: (input: ProviderInput) => Promise<void>
}

const EMPTY: ProviderFormData = { providerType: '', displayName: '', apiBase: '', apiKey: '', enabled: true }

export function ProviderFormDialog({ open, onOpenChange, provider, onSubmit }: ProviderFormDialogProps) {
  const isEditing = !!provider
  const { t } = useTranslation(['providers', 'common'])
  const [submitError, setSubmitError] = useState<string | null>(null)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<ProviderVerifyResult | { valid: false; error: string } | null>(null)

  const { register, handleSubmit, watch, setValue, reset, formState: { errors, isSubmitting, isValid } } = useForm<ProviderFormData>({
    resolver: zodResolver(providerFormSchema),
    mode: 'onChange',
    defaultValues: EMPTY,
  })

  useEffect(() => {
    if (!open) return
    setSubmitError(null)
    setTestResult(null)
    reset({
      providerType: provider?.provider_type ?? '',
      displayName: provider?.display_name ?? '',
      apiBase: provider?.api_base ?? '',
      apiKey: '',
      enabled: provider?.enabled ?? true,
    })
  }, [open, provider, reset])

  const providerType = watch('providerType')
  const displayName = watch('displayName')
  const apiBase = watch('apiBase')
  const enabled = watch('enabled')

  // Pre-fill the API base from the chosen type while creating.
  const typeInfo = useMemo(() => PROVIDER_TYPES.find((p) => p.value === providerType), [providerType])
  useEffect(() => {
    if (!isEditing && typeInfo?.apiBase && !apiBase) setValue('apiBase', typeInfo.apiBase, { shouldValidate: true })
  }, [typeInfo, isEditing, apiBase, setValue])

  const name = useMemo(() => {
    if (isEditing) return provider!.name
    return slugify(displayName || providerType) || 'provider'
  }, [isEditing, provider, displayName, providerType])

  const onValid = async (data: ProviderFormData) => {
    setSubmitError(null)
    const input: ProviderInput = {
      name,
      display_name: data.displayName || undefined,
      provider_type: data.providerType,
      api_base: data.apiBase.trim() || undefined,
      enabled: data.enabled,
    }
    if (data.apiKey) input.api_key = data.apiKey
    try {
      await onSubmit(input)
      onOpenChange(false)
    } catch (err) {
      setSubmitError((err as Error).message || t('providers:form.errors.saveFailed'))
    }
  }

  const testConnection = async () => {
    if (!provider) return
    setTesting(true)
    setTestResult(null)
    try {
      setTestResult(await providerService.verify(provider.id))
    } catch (err) {
      setTestResult({ valid: false, error: (err as Error).message })
    } finally {
      setTesting(false)
    }
  }

  const fieldError = (key: 'providerType' | 'apiBase') => (errors[key]?.message ? t(`providers:${errors[key]!.message}`) : undefined)
  const errorList = (['providerType', 'apiBase'] as const).flatMap((k) => (errors[k]?.message ? [t(`providers:${errors[k]!.message}`)] : []))
  const summary = errorList.length > 0
    ? { title: t('providers:form.errors.summary', { count: errorList.length }), items: errorList }
    : submitError ? { title: t('providers:form.errors.saveFailed'), items: [submitError] } : null

  const typeOptions = PROVIDER_TYPES.map((p) => ({ value: p.value, label: p.label }))
  const busy = isSubmitting

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={isEditing ? t('providers:form.editTitle') : t('providers:form.createTitle')}
      description={t('providers:form.configure')}
      dismissible={!busy}
      footer={(
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)} disabled={busy}>{t('providers:form.cancel')}</Button>
          <Button variant="primary" onClick={handleSubmit(onValid)} loading={busy} disabled={!isValid}>
            {isEditing ? t('providers:form.save') : t('providers:form.create')}
          </Button>
        </DialogFooter>
      )}
    >
      <form className="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void handleSubmit(onValid)() }} noValidate>
        {summary && (
          <div role="alert" className="flex items-start gap-2 rounded-md bg-danger-subtle px-3 py-2.5 text-danger">
            <IconAlertTriangle size={16} className="mt-0.5 shrink-0" />
            <div className="min-w-0">
              <p className="text-ui font-semibold">{summary.title}</p>
              <ul className="mt-0.5 list-disc pl-4 text-caption">
                {summary.items.map((m) => <li key={m}>{m}</li>)}
              </ul>
            </div>
          </div>
        )}

        <Field label={t('providers:form.providerType')} required error={fieldError('providerType')}>
          <Select
            value={providerType}
            onChange={(e) => setValue('providerType', e.target.value, { shouldValidate: true })}
            options={typeOptions}
            placeholder={t('common:selectProvider')}
            disabled={isEditing}
          />
        </Field>

        {isEditing && provider && (
          <Field label={t('providers:form.transportAuth')}>
            <div><ProviderMetaChips provider={provider} /></div>
          </Field>
        )}

        <Field label={t('providers:form.displayName')}>
          <Input {...register('displayName')} placeholder={typeInfo?.label ?? t('providers:form.displayNamePlaceholder')} />
        </Field>

        <Field label={t('providers:form.apiBase')} error={fieldError('apiBase')}>
          <Input {...register('apiBase')} placeholder="https://api.example.com" className="text-code" inputMode="url" />
        </Field>

        <Field
          label={t('providers:form.apiKey')}
          helper={isEditing ? t('providers:form.apiKeyEditPlaceholder') : t('providers:form.apiKeyStored')}
        >
          <Input type="password" autoComplete="off" {...register('apiKey')} placeholder={isEditing ? '••••••••' : t('providers:form.apiKeyPlaceholder')} className="text-code" />
        </Field>

        <div className="flex items-center gap-2">
          <Switch checked={enabled} onCheckedChange={(v) => setValue('enabled', v)} aria-label={t('providers:form.enabled')} />
          <span className="text-ui text-ink-secondary">{t('providers:form.enabled')}</span>
        </div>

        {isEditing && (
          <div>
            <Button variant="secondary" onClick={testConnection} loading={testing} disabled={busy}>
              {t('providers:form.testConnection')}
            </Button>
          </div>
        )}

        {testResult && (
          <p
            role={testResult.valid ? 'status' : 'alert'}
            className={testResult.valid
              ? 'flex items-start gap-1.5 text-caption text-success'
              : 'flex items-start gap-1.5 text-caption text-danger'}
          >
            {testResult.valid ? <IconCheckCircle size={14} className="mt-px shrink-0" /> : <IconXCircle size={14} className="mt-px shrink-0" />}
            <span>
              {testResult.valid
                ? t('providers:form.testOk')
                : `${t('providers:form.testFailed')}${testResult.error ? `: ${testResult.error}` : ''}`}
            </span>
          </p>
        )}
      </form>
    </Dialog>
  )
}

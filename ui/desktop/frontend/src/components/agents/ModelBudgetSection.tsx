import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Field, Input } from '../ui'
import { Combobox } from '../common/Combobox'
import { CatalogueNotice } from './CatalogueNotice'
import { SectionCard } from './section-card'
import { useProviderCapabilities } from '../../hooks/use-provider-capabilities'
import { isCatalogueStale, modelOptionsFor, providerOptionsFrom } from '../../api/provider-capabilities'
import { getApiClient } from '../../lib/api'

export const CONTEXT_WINDOW_RANGE = { min: 1000, max: 1_000_000 } as const
export const MAX_TOOL_ITERATIONS_RANGE = { min: 1, max: 200 } as const

export interface ModelBudgetErrors {
  provider?: string
  model?: string
  contextWindow?: string
  maxToolIterations?: string
}

interface ModelBudgetSectionProps {
  provider: string
  model: string
  contextWindow: number
  maxToolIterations: number
  savedProvider: string
  savedModel: string
  errors?: ModelBudgetErrors
  readOnly?: boolean
  onProviderChange: (v: string) => void
  onModelChange: (v: string) => void
  onContextWindowChange: (v: number) => void
  onMaxToolIterationsChange: (v: number) => void
  onSaveBlockedChange: (blocked: boolean) => void
}

export function ModelBudgetSection({
  provider, model, contextWindow, maxToolIterations,
  savedProvider, savedModel, errors = {}, readOnly = false,
  onProviderChange, onModelChange, onContextWindowChange, onMaxToolIterationsChange,
  onSaveBlockedChange,
}: ModelBudgetSectionProps) {
  const { t } = useTranslation(['agents', 'common'])
  const { capabilities, isLoading, error, refetch } = useProviderCapabilities()
  const [verifyResult, setVerifyResult] = useState<{ valid: boolean; error?: string } | null>(null)
  const [verifying, setVerifying] = useState(false)

  // Provider identity in agent configs is the provider name (`capabilities[].id`).
  const capability = useMemo(
    () => capabilities.find((c) => c.id === provider),
    [capabilities, provider],
  )

  const providerOptions = useMemo(() => providerOptionsFrom(capabilities), [capabilities])
  const modelOptions = useMemo(() => modelOptionsFor(capability), [capability])
  const stale = isCatalogueStale(capability)

  // A changed provider or model must be checked before it can be saved.
  const needsVerify = Boolean((provider !== savedProvider || model !== savedModel) && provider && model)
  useEffect(() => {
    if (needsVerify) {
      setVerifyResult(null)
      onSaveBlockedChange(true)
    } else {
      onSaveBlockedChange(false)
    }
  }, [needsVerify, onSaveBlockedChange])

  const handleVerify = async () => {
    const providerRef = capability?.provider_id ?? capability?.id
    if (!providerRef || !model.trim()) return
    setVerifying(true)
    try {
      const res = await getApiClient().post<{ success: boolean; error?: string }>(
        `/v1/providers/${providerRef}/verify`,
        { model: model.trim() },
      )
      setVerifyResult({ valid: res.success, error: res.error })
      onSaveBlockedChange(!res.success)
    } catch (err) {
      setVerifyResult({ valid: false, error: err instanceof Error ? err.message : t('common:verificationFailed', 'Verification failed') })
    } finally {
      setVerifying(false)
    }
  }

  const modelError = errors.model ?? (verifyResult && !verifyResult.valid ? verifyResult.error : undefined)

  return (
    <SectionCard title={t('agents:screen.detail.modelBudget')}>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Field label={t('common:provider')} required error={errors.provider}>
          <Combobox value={provider} onChange={onProviderChange} options={providerOptions} placeholder={t('agents:create.selectProvider')} disabled={readOnly} />
        </Field>
        <Field label={t('common:model')} required error={modelError}>
          <div className="flex items-start gap-2">
            <div className="min-w-0 flex-1">
              <Combobox
                value={model}
                onChange={onModelChange}
                options={modelOptions}
                placeholder={isLoading ? t('common:loading') : t('agents:create.enterOrSelectModel')}
                loading={isLoading}
                allowCustom
                disabled={readOnly}
              />
            </div>
            {needsVerify && (
              <Button onClick={handleVerify} loading={verifying} disabled={readOnly}>
                {verifying ? t('agents:screen.detail.checking') : t('agents:screen.detail.check')}
              </Button>
            )}
          </div>
        </Field>
      </div>

      <CatalogueNotice stale={stale} error={error} onRetry={() => { void refetch() }} />

      {needsVerify && verifyResult?.valid && (
        <p role="status" className="rounded-md bg-success-subtle px-3 py-2 text-caption text-success">{t('agents:screen.detail.modelVerified')}</p>
      )}
      {needsVerify && !verifyResult && (
        <p className="text-caption text-warning">{t('agents:screen.detail.checkModel')}</p>
      )}

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Field label={t('agents:llmConfig.contextWindow')} helper={t('agents:llmConfig.contextWindowHint')} error={errors.contextWindow}>
          <Input
            type="number"
            inputMode="numeric"
            value={contextWindow || ''}
            onChange={(e) => onContextWindowChange(Number(e.target.value) || 0)}
            placeholder="200000"
            className="font-mono"
            disabled={readOnly}
          />
        </Field>
        <Field label={t('agents:llmConfig.maxToolIterations')} helper={t('agents:llmConfig.maxToolIterationsHint')} error={errors.maxToolIterations}>
          <Input
            type="number"
            inputMode="numeric"
            value={maxToolIterations || ''}
            onChange={(e) => onMaxToolIterationsChange(Number(e.target.value) || 0)}
            placeholder="25"
            className="font-mono"
            disabled={readOnly}
          />
        </Field>
      </div>
    </SectionCard>
  )
}

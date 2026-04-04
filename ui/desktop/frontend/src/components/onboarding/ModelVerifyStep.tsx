import { useEffect, useState, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { getApiClient } from '../../lib/api'
import { Combobox } from '../common/Combobox'
import type { ProviderData } from '../../types/provider'

const VERIFY_TIMEOUT_SECS = 30

interface ModelVerifyStepProps {
  provider: ProviderData
  initialModel?: string | null
  onBack: () => void
  onComplete: (model: string) => void
}

export function ModelVerifyStep({ provider, initialModel, onBack, onComplete }: ModelVerifyStepProps) {
  const { t } = useTranslation(['desktop', 'common'])
  const [models, setModels] = useState<string[]>([])
  const [model, setModel] = useState(initialModel ?? '')
  const [loading, setLoading] = useState(true)
  const [verifying, setVerifying] = useState(false)
  const [verified, setVerified] = useState(false)
  const [error, setError] = useState('')
  const [countdown, setCountdown] = useState(0)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Fetch available models
  useEffect(() => {
    if (!provider.id) return
    setLoading(true)
    console.info('[models] fetching for provider:', provider.id, provider.name)
    getApiClient()
      .get<{ models?: { id: string; name?: string }[] }>(`/v1/providers/${provider.id}/models`)
      .then((res) => {
        console.info('[models] response:', res)
        const ids = (res.models ?? []).map((m) => m.id)
        setModels(ids)
        if (!model && ids.length > 0) setModel(ids[0])
      })
      .catch((err) => {
        console.warn('[models] fetch failed:', err)
      })
      .finally(() => setLoading(false))
  }, [provider.id])

  // Reset verification when model changes
  useEffect(() => {
    setVerified(false)
    setError('')
  }, [model])

  // Countdown timer during verification
  useEffect(() => {
    if (verifying) {
      setCountdown(VERIFY_TIMEOUT_SECS)
      timerRef.current = setInterval(() => {
        setCountdown((prev) => (prev <= 1 ? 0 : prev - 1))
      }, 1000)
    } else {
      setCountdown(0)
      if (timerRef.current) { clearInterval(timerRef.current); timerRef.current = null }
    }
    return () => { if (timerRef.current) { clearInterval(timerRef.current); timerRef.current = null } }
  }, [verifying])

  const handleVerify = async () => {
    if (!model.trim()) return
    setVerifying(true)
    setError('')
    setVerified(false)
    try {
      const result = await getApiClient().post<{ valid: boolean; error?: string }>(
        `/v1/providers/${provider.id}/verify`,
        { model: model.trim() }
      )
      if (result.valid) {
        setVerified(true)
      } else {
        setError(result.error ?? t('desktop:agent.verifyFailed'))
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : t('desktop:agent.verifyFailed'))
    } finally {
      setVerifying(false)
    }
  }

  const providerLabel = provider.display_name || provider.name

  const verifyButtonLabel = verifying
    ? `${t('desktop:agent.verifying')} (${countdown}s)`
    : verified
      ? t('desktop:agent.verified')
      : t('desktop:agent.verifyModel')

  return (
    <div className="bg-surface-secondary border border-border rounded-xl p-6 space-y-4">
      <div>
        <h2 className="text-lg font-semibold text-text-primary">{t('desktop:onboarding.modelStep')}</h2>
        <p className="text-sm text-text-muted">{t('desktop:onboarding.modelStepDesc')}</p>
      </div>

      {/* Provider badge */}
      <div className="flex items-center gap-2">
        <span className="text-sm text-text-muted">{t('common:provider')}</span>
        <span className="text-xs font-medium px-2 py-0.5 rounded-md bg-surface-tertiary border border-border text-text-secondary">
          {providerLabel}
        </span>
      </div>

      {/* Model selection */}
      <div className="space-y-1.5">
        <label className="block text-sm font-medium text-text-secondary">{t('common:model')}</label>
        <Combobox
          value={model}
          onChange={setModel}
          options={models.map((m) => ({ value: m, label: m }))}
          placeholder={loading ? t('common:loadingModels') : t('common:enterOrSelectModel')}
          loading={loading}
          allowCustom
        />
        {!loading && models.length === 0 && (
          <p className="text-xs text-text-muted">
            {t('common:noModelsManualEntry')}
          </p>
        )}
      </div>

      {/* Verification status */}
      {error && (
        <p className="text-sm text-error">{error}</p>
      )}
      {verified && (
        <div className="flex items-center gap-2 rounded-lg border border-success/30 bg-success/10 p-3">
          <svg width="20" height="20" viewBox="0 0 20 20" fill="none" className="text-success flex-shrink-0">
            <path d="M4 10L8 14L16 6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          <div>
            <p className="text-sm font-medium text-success">{t('common:modelVerified')}</p>
            <p className="text-xs text-text-muted">{t('common:modelVerifiedWith', { model, provider: providerLabel })}</p>
          </div>
        </div>
      )}

      <div className="flex justify-between gap-2">
        <button
          onClick={onBack}
          className="px-4 py-2.5 border border-border rounded-lg text-sm font-medium text-text-secondary hover:bg-surface-tertiary transition-colors"
        >
          &larr; {t('common:back')}
        </button>
        <div className="flex gap-2">
          <button
            onClick={handleVerify}
            disabled={!model.trim() || verifying || verified}
            className="px-4 py-2.5 border border-border rounded-lg text-sm font-medium text-text-secondary hover:border-accent hover:text-text-primary transition-colors disabled:opacity-40 disabled:cursor-not-allowed flex items-center gap-2"
          >
            {verifying && <div className="w-3.5 h-3.5 border-2 border-current border-t-transparent rounded-full animate-spin" />}
            {verifyButtonLabel}
          </button>
          <button
            onClick={() => onComplete(model.trim())}
            disabled={!verified}
            className="px-6 py-2.5 bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
          >
            {t('common:continue')}
          </button>
        </div>
      </div>
    </div>
  )
}

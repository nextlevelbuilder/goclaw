import { useEffect, useState, useRef } from 'react'
import { useTranslation } from 'react-i18next'
import { getApiClient } from '../../lib/api'
import { IconCheckCircle } from '../common/Icons'
import { Button, Field, Input } from '../ui'
import { StepError, StepShell } from './onboarding-shell'
import type { ProviderData } from '../../types/provider'

const VERIFY_TIMEOUT_SECS = 30

interface ModelVerifyStepProps {
  provider: ProviderData
  initialModel?: string | null
  onBack: () => void
  onComplete: (model: string) => void
}

export function ModelVerifyStep({ provider, initialModel, onBack, onComplete }: ModelVerifyStepProps) {
  const { t } = useTranslation('onboarding')
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
    getApiClient()
      .get<{ models?: { id: string; name?: string }[] }>(`/v1/providers/${provider.id}/models`)
      .then((res) => {
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
        setError(result.error ?? t('model.verifyFailed'))
      }
    } catch (err) {
      setError(err instanceof Error && err.message ? err.message : t('model.verifyFailed'))
    } finally {
      setVerifying(false)
    }
  }

  const providerLabel = provider.display_name || provider.name
  const listId = 'onboarding-model-list'

  return (
    <StepShell title={t('model.title')} description={t('model.description')}>
      <p className="flex items-center gap-2 text-ui text-ink-secondary">
        {t('model.provider')}
        <span className="rounded-pill border border-border-subtle bg-surface-raised px-2 py-0.5 text-caption font-medium text-ink">{providerLabel}</span>
      </p>

      <Field label={t('model.label')} helper={!loading && models.length === 0 ? t('model.noModels') : undefined}>
        <Input
          value={model}
          list={listId}
          disabled={loading}
          className="font-mono"
          placeholder={loading ? t('model.loading') : t('model.placeholder')}
          onChange={(e) => setModel(e.target.value)}
        />
      </Field>
      <datalist id={listId}>
        {models.map((m) => <option key={m} value={m} />)}
      </datalist>

      {error && <StepError>{error}</StepError>}
      {verified && (
        <div role="status" className="flex items-center gap-2 rounded-md bg-success-subtle px-3 py-2.5 text-success">
          <IconCheckCircle size={18} className="shrink-0" />
          <div>
            <p className="text-ui font-medium">{t('model.verifiedTitle')}</p>
            <p className="text-caption text-ink-secondary">{t('model.verifiedBody', { model, provider: providerLabel })}</p>
          </div>
        </div>
      )}

      <div className="flex items-center justify-between gap-2">
        <Button variant="ghost" onClick={onBack}>{t('back')}</Button>
        <div className="flex gap-2">
          <Button
            onClick={() => { void handleVerify() }}
            disabled={!model.trim() || verified}
            loading={verifying}
          >
            {verifying ? t('model.verifying', { seconds: countdown }) : verified ? t('model.verified') : t('model.verify')}
          </Button>
          <Button variant="primary" onClick={() => onComplete(model.trim())} disabled={!verified}>{t('continue')}</Button>
        </div>
      </div>
    </StepShell>
  )
}

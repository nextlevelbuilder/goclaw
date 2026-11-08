import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { getApiClient } from '../../lib/api'
import { PROVIDER_TYPES } from '../../constants/providers'
import { slugify } from '../../lib/slug'
import { Button, Field, Input, Select } from '../ui'
import { StepError, StepShell } from './onboarding-shell'
import type { ProviderData } from '../../types/provider'

interface ProviderStepProps {
  existingProvider?: ProviderData | null
  onComplete: (provider: ProviderData) => void
}

const TYPE_OPTIONS = PROVIDER_TYPES.map((p) => ({ value: p.value, label: p.label }))

export function ProviderStep({ existingProvider, onComplete }: ProviderStepProps) {
  const { t } = useTranslation('onboarding')
  const isEditing = !!existingProvider

  const [providerType, setProviderType] = useState(existingProvider?.provider_type ?? 'openrouter')
  const [name, setName] = useState(existingProvider?.name ?? 'openrouter')
  const [apiKey, setApiKey] = useState('')
  const [apiBase, setApiBase] = useState(
    existingProvider?.api_base ?? 'https://openrouter.ai/api/v1'
  )
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const [keyError, setKeyError] = useState('')

  const preset = PROVIDER_TYPES.find((p) => p.value === providerType)
  const needsKey = preset?.needsKey ?? true

  const handleTypeChange = (value: string) => {
    setProviderType(value)
    const p = PROVIDER_TYPES.find((x) => x.value === value)
    setName(slugify(value))
    setApiBase(p?.apiBase || '')
    setApiKey('')
    setError('')
    setKeyError('')
  }

  const handleSubmit = async () => {
    if (!isEditing && needsKey && !apiKey.trim()) {
      setKeyError(t('provider.apiKeyRequired'))
      return
    }
    setLoading(true)
    setError('')
    try {
      const api = getApiClient()
      if (isEditing) {
        const patch: Record<string, unknown> = {
          name: name.trim(),
          provider_type: providerType,
          api_base: apiBase.trim() || undefined,
        }
        if (apiKey.trim()) patch.api_key = apiKey.trim()
        await api.put(`/v1/providers/${existingProvider!.id}`, patch)
        onComplete({ ...existingProvider!, ...patch } as ProviderData)
      } else {
        const payload: Record<string, unknown> = {
          name: name.trim(),
          provider_type: providerType,
          api_base: apiBase.trim() || undefined,
          enabled: true,
        }
        if (needsKey) payload.api_key = apiKey.trim()

        // Check if provider with same name already exists → update instead of create
        const list = await api.get<{ providers?: ProviderData[] | null }>('/v1/providers')
        const existing = (list.providers ?? []).find((p) => p.name === name.trim())
        if (existing) {
          await api.put(`/v1/providers/${existing.id}`, payload)
          onComplete({ ...existing, ...payload } as ProviderData)
        } else {
          const result = await api.post<ProviderData>('/v1/providers', payload)
          onComplete(result)
        }
      }
    } catch (err) {
      setError(err instanceof Error && err.message ? err.message : t('provider.saveFailed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <StepShell title={t('provider.title')} description={t('provider.description')}>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <Field label={t('provider.type')}>
          <Select value={providerType} options={TYPE_OPTIONS} onChange={(e) => handleTypeChange(e.target.value)} />
        </Field>
        <Field label={t('provider.name')}>
          <Input value={name} onChange={(e) => setName(slugify(e.target.value))} className="font-mono" />
        </Field>
      </div>

      {needsKey ? (
        <>
          <Field label={t('provider.apiKey')} helper={t('provider.apiKeyHelper')} error={keyError} required>
            <Input
              type="password"
              value={apiKey}
              autoComplete="off"
              placeholder="sk-..."
              onChange={(e) => { setApiKey(e.target.value); setError(''); setKeyError('') }}
            />
          </Field>
          <Field label={t('provider.apiBase')}>
            <Input value={apiBase} placeholder="https://api.example.com/v1" onChange={(e) => setApiBase(e.target.value)} />
          </Field>
        </>
      ) : (
        <p className="text-ui text-ink-secondary">{t('provider.noKey', { provider: preset?.label })}</p>
      )}

      {error && <StepError>{error}</StepError>}

      <div className="flex justify-end">
        <Button variant="primary" onClick={() => { void handleSubmit() }} loading={loading}>
          {isEditing ? t('provider.update') : t('provider.save')}
        </Button>
      </div>
    </StepShell>
  )
}

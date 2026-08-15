import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { getApiClient } from '../../lib/api'
import { PROVIDER_TYPES } from '../../constants/providers'
import { slugify } from '../../lib/slug'
import { Combobox } from '../common/Combobox'
import type { ProviderData } from '../../types/provider'

interface ProviderStepProps {
  existingProvider?: ProviderData | null
  onComplete: (provider: ProviderData) => void
}

export function ProviderStep({ existingProvider, onComplete }: ProviderStepProps) {
  const { t } = useTranslation(['desktop', 'common'])
  const isEditing = !!existingProvider

  const [providerType, setProviderType] = useState(existingProvider?.provider_type ?? 'openrouter')
  const [name, setName] = useState(existingProvider?.name ?? 'openrouter')
  const [apiKey, setApiKey] = useState('')
  const [apiBase, setApiBase] = useState(
    existingProvider?.api_base ?? 'https://openrouter.ai/api/v1'
  )
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const preset = PROVIDER_TYPES.find((t) => t.value === providerType)
  const needsKey = preset?.needsKey ?? true

  const handleTypeChange = (value: string) => {
    setProviderType(value)
    const p = PROVIDER_TYPES.find((t) => t.value === value)
    setName(slugify(value))
    setApiBase(p?.apiBase || '')
    setApiKey('')
    setError('')
  }

  const handleSubmit = async () => {
    if (!isEditing && needsKey && !apiKey.trim()) {
      setError(t('common:apiKeyRequired'))
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
      setError(err instanceof Error ? err.message : t('common:failedToCreateProvider'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="bg-surface-secondary border border-border rounded-xl p-6 space-y-4">
      <div>
        <h2 className="text-lg font-semibold text-text-primary">{t('desktop:onboarding.providerStep')}</h2>
        <p className="text-sm text-text-muted">{t('desktop:onboarding.providerStepDesc')}</p>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        {/* Provider type select */}
        <div className="space-y-1.5">
          <label className="block text-sm font-medium text-text-secondary">{t('common:providerType')}</label>
          <Combobox
            value={providerType}
            onChange={handleTypeChange}
            options={PROVIDER_TYPES.map((t) => ({ value: t.value, label: t.label }))}
            placeholder={t('common:searchProviders')}
            allowCustom={false}
          />
        </div>

        {/* Name (slug) */}
        <div className="space-y-1.5">
          <label className="block text-sm font-medium text-text-secondary">{t('common:name')}</label>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(slugify(e.target.value))}
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2.5 text-base md:text-sm text-text-primary font-mono focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>
      </div>

      {/* API Key */}
      {needsKey && (
        <div className="space-y-1.5">
          <label className="block text-sm font-medium text-text-secondary">{t('common:apiKey')}</label>
          <input
            type="password"
            value={apiKey}
            onChange={(e) => { setApiKey(e.target.value); setError('') }}
            placeholder="sk-..."
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2.5 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>
      )}

      {/* API Base */}
      {needsKey && (
        <div className="space-y-1.5">
          <label className="block text-sm font-medium text-text-secondary">{t('common:apiBaseUrl')}</label>
          <input
            type="text"
            value={apiBase}
            onChange={(e) => setApiBase(e.target.value)}
            placeholder="https://api.example.com/v1"
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2.5 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>
      )}

      {!needsKey && (
        <p className="text-sm text-text-muted">{t('common:noApiKeyRequired', { provider: preset?.label })}</p>
      )}

      {error && <p className="text-sm text-error">{error}</p>}

      <div className="flex justify-end">
        <button
          onClick={handleSubmit}
          disabled={loading || (!isEditing && needsKey && !apiKey.trim())}
          className="px-6 py-2.5 bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-40 disabled:cursor-not-allowed flex items-center gap-2"
        >
          {loading && <div className="w-3.5 h-3.5 border-2 border-white border-t-transparent rounded-full animate-spin" />}
          {isEditing ? t('common:update') : t('desktop:onboarding.createProvider')}
        </button>
      </div>
    </div>
  )
}

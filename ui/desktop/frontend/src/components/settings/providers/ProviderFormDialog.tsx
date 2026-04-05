import { useState, useEffect, useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { Combobox } from '../../common/Combobox'
import { Switch } from '../../common/Switch'
import { PROVIDER_TYPES } from '../../../constants/providers'
import { slugify } from '../../../lib/slug'
import type { ProviderData, ProviderInput } from '../../../types/provider'

interface ProviderFormDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** If set, editing this provider. Otherwise creating. */
  provider?: ProviderData | null
  onSubmit: (input: ProviderInput) => Promise<void>
}

export function ProviderFormDialog({ open, onOpenChange, provider, onSubmit }: ProviderFormDialogProps) {
  const isEditing = !!provider
  const { t } = useTranslation(['providers', 'common'])

  const [providerType, setProviderType] = useState(provider?.provider_type ?? '')
  const [displayName, setDisplayName] = useState(provider?.display_name ?? '')
  const [apiBase, setApiBase] = useState(provider?.api_base ?? '')
  const [apiKey, setApiKey] = useState('')
  const [enabled, setEnabled] = useState(provider?.enabled ?? true)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  // Reset form when dialog opens/closes or provider changes
  useEffect(() => {
    if (open) {
      setProviderType(provider?.provider_type ?? '')
      setDisplayName(provider?.display_name ?? '')
      setApiBase(provider?.api_base ?? '')
      setApiKey('')
      setEnabled(provider?.enabled ?? true)
      setError('')
    }
  }, [open, provider])

  // Auto-set api_base from provider type
  const typeInfo = useMemo(() => PROVIDER_TYPES.find((t) => t.value === providerType), [providerType])
  useEffect(() => {
    if (!isEditing && typeInfo?.apiBase && !apiBase) {
      setApiBase(typeInfo.apiBase)
    }
  }, [typeInfo, isEditing, apiBase])

  const name = useMemo(() => {
    if (isEditing) return provider!.name
    return slugify(displayName || providerType) || 'provider'
  }, [isEditing, provider, displayName, providerType])

  const handleSubmit = async () => {
    setLoading(true)
    setError('')
    try {
      const input: ProviderInput = {
        name,
        display_name: displayName || undefined,
        provider_type: providerType,
        api_base: apiBase || undefined,
        enabled,
      }
      // Only send api_key if user entered a new one
      if (apiKey) input.api_key = apiKey
      await onSubmit(input)
      onOpenChange(false)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save provider')
    } finally {
      setLoading(false)
    }
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
            onChange={setProviderType}
            options={typeOptions}
            placeholder={t('common:selectProvider')}
            disabled={isEditing}
          />
        </div>

        {/* Display name */}
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('providers:form.displayName')}</label>
          <input
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder={typeInfo?.label ?? 'My Provider'}
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>

        {/* API Base */}
        <div className="space-y-1">
          <label className="text-xs font-medium text-text-secondary">{t('providers:form.apiBase')}</label>
          <input
            value={apiBase}
            onChange={(e) => setApiBase(e.target.value)}
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
            value={apiKey}
            onChange={(e) => setApiKey(e.target.value)}
            placeholder={isEditing ? '••••••••' : 'sk-...'}
            className="w-full bg-surface-tertiary border border-border rounded-lg px-3 py-2 text-base md:text-sm text-text-primary font-mono placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
          />
        </div>

        {/* Enabled toggle */}
        <div className="flex items-center gap-2">
          <Switch checked={enabled} onCheckedChange={setEnabled} />
          <span className="text-xs text-text-secondary">{t('providers:form.enabled')}</span>
        </div>

        {error && <p className="text-xs text-error">{error}</p>}

        <div className="flex justify-end gap-2 pt-1">
          <button
            onClick={() => onOpenChange(false)}
            className="px-3 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors"
          >
            {t('providers:form.cancel')}
          </button>
          <button
            onClick={handleSubmit}
            disabled={loading || !providerType}
            className="px-4 py-1.5 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-50"
          >
            {loading ? '...' : isEditing ? t('providers:form.save') : t('providers:form.create')}
          </button>
        </div>
      </div>
    </div>
  )
}

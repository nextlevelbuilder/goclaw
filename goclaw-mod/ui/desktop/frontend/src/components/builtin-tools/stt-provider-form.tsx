import { useState } from 'react'
import { useTranslation } from 'react-i18next'

type SttProviderName = 'elevenlabs' | 'proxy'

const ALL_STT_PROVIDERS: SttProviderName[] = ['elevenlabs', 'proxy']

interface SttSettings {
  providers: SttProviderName[]
  elevenlabs: { api_key: string; default_language: string }
  proxy: { url: string; api_key: string; tenant_id: string }
  whatsapp_enabled: boolean
}

interface Props {
  initialSettings: Record<string, unknown>
  onSave: (settings: Record<string, unknown>) => Promise<void>
  onCancel: () => void
}

function resolveInitial(raw: Record<string, unknown>): SttSettings {
  const el = (raw.elevenlabs as Record<string, unknown>) ?? {}
  const px = (raw.proxy as Record<string, unknown>) ?? {}
  return {
    providers: (raw.providers as SttProviderName[]) ?? ['elevenlabs', 'proxy'],
    elevenlabs: {
      api_key: (el.api_key as string) ?? '',
      default_language: (el.default_language as string) ?? 'en',
    },
    proxy: {
      url: (px.url as string) ?? '',
      api_key: (px.api_key as string) ?? '',
      tenant_id: (px.tenant_id as string) ?? '',
    },
    whatsapp_enabled: (raw.whatsapp_enabled as boolean) ?? false,
  }
}

function buildPayload(s: SttSettings): Record<string, unknown> {
  return {
    providers: s.providers,
    elevenlabs: { api_key: s.elevenlabs.api_key, default_language: s.elevenlabs.default_language },
    proxy: { url: s.proxy.url, api_key: s.proxy.api_key, tenant_id: s.proxy.tenant_id },
    whatsapp_enabled: s.whatsapp_enabled,
  }
}

export function SttProviderForm({ initialSettings, onSave, onCancel }: Props) {
  const { t } = useTranslation('tools')
  const init = resolveInitial(initialSettings)

  const [providers, setProviders] = useState<SttProviderName[]>(init.providers)
  const [elApiKey, setElApiKey] = useState(init.elevenlabs.api_key)
  const [elLang, setElLang] = useState(init.elevenlabs.default_language)
  const [proxyUrl, setProxyUrl] = useState(init.proxy.url)
  const [proxyApiKey, setProxyApiKey] = useState(init.proxy.api_key)
  const [proxyTenantId, setProxyTenantId] = useState(init.proxy.tenant_id)
  const [whatsappEnabled, setWhatsappEnabled] = useState(init.whatsapp_enabled)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')

  const toggleProvider = (name: SttProviderName) => {
    setProviders((prev) =>
      prev.includes(name) ? prev.filter((p) => p !== name) : [...prev, name],
    )
  }

  const handleSave = async () => {
    if (providers.length === 0) {
      setError(t('builtin.sttForm.providersRequiredError'))
      return
    }
    setError('')
    setSaving(true)
    try {
      await onSave(buildPayload({
        providers,
        elevenlabs: { api_key: elApiKey, default_language: elLang },
        proxy: { url: proxyUrl, api_key: proxyApiKey, tenant_id: proxyTenantId },
        whatsapp_enabled: whatsappEnabled,
      }))
    } catch {
      // error surfaced by parent
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="flex flex-col overflow-hidden">
      {/* Scrollable body */}
      <div className="flex-1 overflow-y-auto px-5 py-4 space-y-5">
        <p className="text-xs text-text-muted">{t('builtin.sttForm.description')}</p>

        {/* Providers */}
        <div className="space-y-1.5">
          <p className="text-xs font-medium text-text-secondary">{t('builtin.sttForm.providersLabel')}</p>
          <div className="flex flex-wrap gap-3">
            {ALL_STT_PROVIDERS.map((p) => (
              <label key={p} className="flex items-center gap-1.5 cursor-pointer select-none text-sm">
                <input
                  type="checkbox"
                  checked={providers.includes(p)}
                  onChange={() => toggleProvider(p)}
                  className="h-4 w-4 rounded"
                />
                <span className="capitalize">{p}</span>
              </label>
            ))}
          </div>
          {error && <p className="text-xs text-red-500">{error}</p>}
        </div>

        {/* ElevenLabs */}
        <div className="space-y-2">
          <p className="text-xs font-semibold text-text-primary">{t('builtin.sttForm.elevenlabsSectionTitle')}</p>
          <div className="space-y-1">
            <label className="text-xs text-text-secondary">{t('builtin.sttForm.elevenlabsApiKeyLabel')}</label>
            <input
              type="password"
              value={elApiKey}
              onChange={(e) => setElApiKey(e.target.value)}
              placeholder="xi-..."
              className="w-full bg-surface-tertiary border border-border rounded-lg px-2.5 py-1.5 text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs text-text-secondary">{t('builtin.sttForm.elevenlabsDefaultLanguageLabel')}</label>
            <input
              type="text"
              value={elLang}
              onChange={(e) => setElLang(e.target.value)}
              placeholder="en"
              className="w-full bg-surface-tertiary border border-border rounded-lg px-2.5 py-1.5 text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
            />
          </div>
        </div>

        {/* Proxy */}
        <div className="space-y-2">
          <p className="text-xs font-semibold text-text-primary">{t('builtin.sttForm.proxySectionTitle')}</p>
          <div className="space-y-1">
            <label className="text-xs text-text-secondary">{t('builtin.sttForm.proxyUrlLabel')}</label>
            <input
              type="url"
              value={proxyUrl}
              onChange={(e) => setProxyUrl(e.target.value)}
              placeholder="https://..."
              className="w-full bg-surface-tertiary border border-border rounded-lg px-2.5 py-1.5 text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs text-text-secondary">{t('builtin.sttForm.proxyApiKeyLabel')}</label>
            <input
              type="password"
              value={proxyApiKey}
              onChange={(e) => setProxyApiKey(e.target.value)}
              className="w-full bg-surface-tertiary border border-border rounded-lg px-2.5 py-1.5 text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
            />
          </div>
          <div className="space-y-1">
            <label className="text-xs text-text-secondary">{t('builtin.sttForm.proxyTenantIdLabel')}</label>
            <input
              type="text"
              value={proxyTenantId}
              onChange={(e) => setProxyTenantId(e.target.value)}
              className="w-full bg-surface-tertiary border border-border rounded-lg px-2.5 py-1.5 text-sm text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent"
            />
          </div>
        </div>

        {/* WhatsApp section — privacy banner ABOVE toggle */}
        <div className="space-y-2">
          {/* Privacy banner */}
          <div
            className="flex items-start gap-2 rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-800"
            data-testid="whatsapp-privacy-banner"
          >
            <svg className="mt-0.5 h-3.5 w-3.5 shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
              <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
              <line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" />
            </svg>
            <span>{t('builtin.sttForm.whatsappPrivacyWarning')}</span>
          </div>
          <label className="flex items-center gap-2 cursor-pointer select-none text-sm">
            <input
              type="checkbox"
              checked={whatsappEnabled}
              onChange={(e) => setWhatsappEnabled(e.target.checked)}
              className="h-4 w-4 rounded"
            />
            <span className="font-medium">{t('builtin.sttForm.whatsappEnabledLabel')}</span>
          </label>
        </div>
      </div>

      {/* Footer */}
      <div className="flex items-center justify-end gap-2 border-t border-border px-5 py-3 bg-surface-secondary">
        <button
          type="button"
          onClick={onCancel}
          className="px-4 py-1.5 text-xs border border-border rounded-lg text-text-secondary hover:bg-surface-tertiary transition-colors"
        >
          {t('builtin.sttForm.cancel')}
        </button>
        <button
          type="button"
          onClick={handleSave}
          disabled={saving}
          className="px-4 py-1.5 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-50 flex items-center gap-1.5"
        >
          {saving && (
            <svg className="w-3 h-3 animate-spin" viewBox="0 0 24 24" fill="none">
              <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" />
              <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8z" />
            </svg>
          )}
          {saving ? t('builtin.sttForm.saving') : t('builtin.sttForm.save')}
        </button>
      </div>
    </div>
  )
}

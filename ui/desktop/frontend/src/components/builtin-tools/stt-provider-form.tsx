import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { IconAlertTriangle } from '../common/Icons'
import { Checkbox, Field, Input } from '../ui'
import { ToolFormFooter } from '../tools/tool-form-footer'

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
    <div className="flex flex-col gap-5">
      <p className="text-caption text-ink-muted">{t('builtin.sttForm.description')}</p>

      <fieldset className="flex flex-col gap-2">
        <legend className="mb-1 text-ui font-medium text-ink">{t('builtin.sttForm.providersLabel')}</legend>
        <div className="flex flex-wrap gap-4">
          {ALL_STT_PROVIDERS.map((p) => (
            <Checkbox
              key={p}
              checked={providers.includes(p)}
              onCheckedChange={() => toggleProvider(p)}
              label={<span className="capitalize">{p}</span>}
            />
          ))}
        </div>
        {error && <p role="alert" className="text-caption text-danger">{error}</p>}
      </fieldset>

      <section className="flex flex-col gap-3">
        <h3 className="text-ui font-semibold text-ink">{t('builtin.sttForm.elevenlabsSectionTitle')}</h3>
        <Field label={t('builtin.sttForm.elevenlabsApiKeyLabel')}>
          <Input type="password" value={elApiKey} onChange={(e) => setElApiKey(e.target.value)} placeholder="xi-..." />
        </Field>
        <Field label={t('builtin.sttForm.elevenlabsDefaultLanguageLabel')}>
          <Input value={elLang} onChange={(e) => setElLang(e.target.value)} placeholder="en" />
        </Field>
      </section>

      <section className="flex flex-col gap-3">
        <h3 className="text-ui font-semibold text-ink">{t('builtin.sttForm.proxySectionTitle')}</h3>
        <Field label={t('builtin.sttForm.proxyUrlLabel')}>
          <Input type="url" value={proxyUrl} onChange={(e) => setProxyUrl(e.target.value)} placeholder="https://..." />
        </Field>
        <Field label={t('builtin.sttForm.proxyApiKeyLabel')}>
          <Input type="password" value={proxyApiKey} onChange={(e) => setProxyApiKey(e.target.value)} />
        </Field>
        <Field label={t('builtin.sttForm.proxyTenantIdLabel')}>
          <Input value={proxyTenantId} onChange={(e) => setProxyTenantId(e.target.value)} />
        </Field>
      </section>

      <section className="flex flex-col gap-2">
        <div
          role="note"
          className="flex items-start gap-2 rounded-md border border-warning bg-warning-subtle px-3 py-2 text-caption text-warning-text"
          data-testid="whatsapp-privacy-banner"
        >
          <IconAlertTriangle size={14} className="mt-0.5 shrink-0" />
          <span>{t('builtin.sttForm.whatsappPrivacyWarning')}</span>
        </div>
        <Checkbox
          checked={whatsappEnabled}
          onCheckedChange={setWhatsappEnabled}
          label={<span className="font-medium">{t('builtin.sttForm.whatsappEnabledLabel')}</span>}
        />
      </section>

      <ToolFormFooter
        cancelLabel={t('builtin.sttForm.cancel')}
        saveLabel={saving ? t('builtin.sttForm.saving') : t('builtin.sttForm.save')}
        onCancel={onCancel}
        onSave={() => void handleSave()}
        saving={saving}
      />
    </div>
  )
}

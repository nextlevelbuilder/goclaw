import { useTranslation } from 'react-i18next'
import { PROVIDER_TYPES } from '../../constants/providers'
import { ProviderMetaChips } from './ProviderMetaChips'
import type { ProviderData } from '../../types/provider'

/** Read-only summary of the provider connection; editing goes through ProviderFormDialog. */
export function ProviderConnectionTab({ provider, onEdit }: { provider: ProviderData; onEdit: () => void }) {
  const { t } = useTranslation('providers')
  const typeLabel = PROVIDER_TYPES.find((p) => p.value === provider.provider_type)?.label ?? provider.provider_type
  const notSet = <span className="text-text-muted">{t('providerDetail.notSet')}</span>

  const rows: { label: string; value: React.ReactNode; mono?: boolean }[] = [
    { label: t('providerDetail.fields.name'), value: provider.name, mono: true },
    { label: t('providerDetail.fields.type'), value: typeLabel },
    { label: t('providerDetail.fields.apiBase'), value: provider.api_base || notSet, mono: !!provider.api_base },
    { label: t('providerDetail.fields.apiKey'), value: provider.api_key ? t('providerDetail.apiKeySet') : t('providerDetail.apiKeyNotSet') },
    {
      label: t('providerDetail.fields.status'),
      value: provider.enabled ? t('providerDetail.status.running') : t('providerDetail.status.disabled'),
    },
    { label: t('providerDetail.fields.defaultModel'), value: provider.default_model_id || notSet, mono: !!provider.default_model_id },
    { label: t('providerDetail.fields.models'), value: provider.models?.length ? String(provider.models.length) : notSet },
  ]

  return (
    <div className="space-y-3">
      <dl className="grid grid-cols-1 sm:grid-cols-[180px_1fr] gap-x-4 gap-y-2 text-xs">
        {rows.map((r) => (
          <div key={r.label} className="contents">
            <dt className="text-text-muted">{r.label}</dt>
            <dd className={`text-text-primary break-all ${r.mono ? 'font-mono' : ''}`}>{r.value}</dd>
          </div>
        ))}
        {(provider.wire_api || provider.auth_kind || provider.model_source) && (
          <>
            <dt className="text-text-muted">{t('providerDetail.fields.transportAuth')}</dt>
            <dd><ProviderMetaChips provider={provider} /></dd>
          </>
        )}
      </dl>
      <button
        onClick={onEdit}
        className="px-3 py-1.5 text-xs bg-accent text-accent-foreground rounded-lg font-medium hover:bg-accent-hover transition-colors"
      >
        {t('providerDetail.edit')}
      </button>
    </div>
  )
}

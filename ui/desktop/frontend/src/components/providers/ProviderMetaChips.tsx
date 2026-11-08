import { useTranslation } from 'react-i18next'
import { AUTH_KIND_LABEL_KEYS, MODEL_SOURCE_LABEL_KEYS, WIRE_API_LABEL_KEYS } from '../../constants/providers'
import { isCatalogueStale } from '../../api/provider-capabilities'
import { Badge } from '../ui'
import { IconAlertTriangle } from '../common/Icons'
import type { ProviderData } from '../../types/provider'

/** Localized label for a gateway enum value, falling back to the raw value. */
function valueLabel(t: (key: string) => string, keys: Record<string, string>, value: string): string {
  const key = keys[value]
  return key ? t(key) : value
}

/**
 * Read-only chips for the provider subsystem metadata: `wire_api`, `auth_kind`,
 * `model_source` — plus a warning when the model catalogue is stale.
 * Rendered everywhere a provider is described (detail, edit dialog).
 */
export function ProviderMetaChips({ provider }: { provider: ProviderData }) {
  const { t } = useTranslation('providers')
  const stale = isCatalogueStale({ id: provider.name, model_source: provider.model_source, models: provider.models })

  const chips: { key: string; label: string; title: string }[] = []
  if (provider.wire_api) {
    chips.push({ key: 'wire_api', label: valueLabel(t, WIRE_API_LABEL_KEYS, provider.wire_api), title: t('meta.wireApi') })
  }
  if (provider.auth_kind) {
    chips.push({ key: 'auth_kind', label: valueLabel(t, AUTH_KIND_LABEL_KEYS, provider.auth_kind), title: t('meta.authKind') })
  }
  if (provider.model_source) {
    chips.push({ key: 'model_source', label: valueLabel(t, MODEL_SOURCE_LABEL_KEYS, provider.model_source), title: t('meta.modelSource') })
  }
  if (chips.length === 0 && !stale) return null

  return (
    <div className="flex flex-wrap items-center gap-1">
      {chips.map((c) => (
        <Badge key={c.key} title={`${c.title}: ${c.label}`}>{c.label}</Badge>
      ))}
      {stale && (
        <Badge tone="warning" title={t('meta.staleCatalogue')} className="gap-1">
          <IconAlertTriangle size={12} />
          {t('meta.staleCatalogue')}
        </Badge>
      )}
    </div>
  )
}

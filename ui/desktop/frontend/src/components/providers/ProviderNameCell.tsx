import { useTranslation } from 'react-i18next'
import { PROVIDER_TYPES } from '../../constants/providers'
import type { ProviderData } from '../../types/provider'

/** Two-letter tile shown before the provider name. */
function initials(name: string): string {
  const parts = name.trim().split(/[\s_-]+/).filter(Boolean)
  return (parts.length > 1 ? parts[0][0] + parts[1][0] : name.slice(0, 2)).toUpperCase()
}

interface ProviderNameCellProps {
  provider: ProviderData
  /** Opens the provider detail view. */
  onOpen: (provider: ProviderData) => void
}

/**
 * Name cell of the providers table: initials tile, a name button and the type label.
 * The button is the keyboard and screen-reader entry point to the detail view;
 * clicking elsewhere on the row is a mouse shortcut handled by the table.
 */
export function ProviderNameCell({ provider, onOpen }: ProviderNameCellProps) {
  const { t } = useTranslation('providers')
  const name = provider.display_name || provider.name
  const typeLabel = PROVIDER_TYPES.find((x) => x.value === provider.provider_type)?.label ?? provider.provider_type

  return (
    <div className="flex min-w-0 items-center gap-3">
      <span aria-hidden="true" className="flex size-8 shrink-0 items-center justify-center rounded-pill bg-accent-subtle text-caption font-semibold text-accent-subtle-ink">
        {initials(name)}
      </span>
      <div className="min-w-0">
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            onOpen(provider)
          }}
          aria-label={t('providerDetail.openDetail', { name })}
          className="block max-w-full truncate rounded-sm text-left text-ui font-medium text-ink hover:underline"
        >
          {name}
        </button>
        <p className="truncate text-caption text-ink-muted">{typeLabel}</p>
      </div>
    </div>
  )
}

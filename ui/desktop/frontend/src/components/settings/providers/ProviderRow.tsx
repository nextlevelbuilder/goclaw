import type { ProviderData } from '../../../types/provider'
import { PROVIDER_TYPES } from '../../../constants/providers'

interface ProviderRowProps {
  provider: ProviderData
  onEdit: (provider: ProviderData) => void
  onDelete: (provider: ProviderData) => void
}

export function ProviderRow({ provider, onEdit, onDelete }: ProviderRowProps) {
  const typeInfo = PROVIDER_TYPES.find((t) => t.value === provider.provider_type)

  return (
    <div className="flex items-center gap-3 px-3 py-2.5 rounded-lg border border-border hover:bg-surface-tertiary/50 transition-colors">
      {/* Status dot */}
      <span className={`w-2 h-2 rounded-full shrink-0 ${provider.enabled ? 'bg-success' : 'bg-error'}`} />

      {/* Info */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-text-primary truncate">
            {provider.display_name || provider.name}
          </span>
          <span className="text-[10px] px-1.5 py-0.5 rounded bg-surface-tertiary text-text-muted shrink-0">
            {typeInfo?.label ?? provider.provider_type}
          </span>
        </div>
        {provider.api_base && (
          <p className="text-[11px] text-text-muted truncate mt-0.5">{provider.api_base}</p>
        )}
      </div>

      {/* Actions */}
      <div className="flex items-center gap-1 shrink-0">
        <button
          onClick={() => onEdit(provider)}
          className="p-1.5 rounded text-text-muted hover:text-text-primary hover:bg-surface-tertiary transition-colors"
          title="Edit"
        >
          <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7" />
            <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z" />
          </svg>
        </button>
        <button
          onClick={() => onDelete(provider)}
          className="p-1.5 rounded text-text-muted hover:text-error hover:bg-surface-tertiary transition-colors"
          title="Delete"
        >
          <svg className="w-3.5 h-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <polyline points="3 6 5 6 21 6" /><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
          </svg>
        </button>
      </div>
    </div>
  )
}

import { useTranslation } from 'react-i18next'

interface CatalogueNoticeProps {
  /** Provider reports a stale model catalogue (stale: true / model_source: stale). */
  stale: boolean
  /** Capabilities request failed. */
  error: string | null
  onRetry: () => void
}

/**
 * Inline status for the model catalogue: a stale catalogue or a failed fetch is
 * surfaced with a retry action instead of degrading to an empty model list.
 */
export function CatalogueNotice({ stale, error, onRetry }: CatalogueNoticeProps) {
  const { t } = useTranslation('agents')
  if (!stale && !error) return null

  return (
    <div className="flex items-center gap-2 text-caption text-warning">
      <span className="truncate" title={error ?? undefined}>
        {error ? t('catalogue.loadFailed') : t('catalogue.stale')}
      </span>
      <button
        type="button"
        onClick={onRetry}
        className="px-2 py-0.5 border border-warning/40 rounded hover:bg-warning/10 transition-colors shrink-0"
      >
        {t('catalogue.retry')}
      </button>
    </div>
  )
}

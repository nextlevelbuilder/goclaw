import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'
import { IconRefresh, IconWifiOff } from '../common/Icons'
import { Button } from './Button'

export interface OfflineBannerProps {
  /** Seconds until the next automatic retry; null while a retry is in flight or unknown. */
  retryInSeconds: number | null
  /** A reconnect attempt is running now. */
  retrying?: boolean
  /** "Retry now" handler. The button is hidden when omitted. */
  onRetry?: () => void
  className?: string
}

/** Gateway unreachable: banner above saved data with automatic-retry text. */
export function OfflineBanner({ retryInSeconds, retrying = false, onRetry, className }: OfflineBannerProps) {
  const { t } = useTranslation('common')
  const detail = retrying || retryInSeconds === null
    ? t('state.offlineRetrying')
    : `${t('state.offlineSaved')} ${t('state.offlineRetryIn', { count: retryInSeconds })}`

  return (
    <div
      role="alert"
      className={cn(
        'flex items-center gap-3 rounded-md bg-danger-subtle py-2.5 pl-3.5 pr-3 text-ui text-danger',
        className,
      )}
    >
      <IconWifiOff size={16} className="shrink-0" />
      <span className="min-w-0 flex-1">
        <span className="block">{t('state.offlineTitle')}</span>
        <span className="block text-caption text-ink-secondary">{detail}</span>
      </span>
      {onRetry && (
        <Button
          size="sm"
          variant="secondary"
          className="border-danger text-danger"
          icon={<IconRefresh size={14} />}
          onClick={onRetry}
          loading={retrying}
        >
          {t('state.retryNow')}
        </Button>
      )}
    </div>
  )
}

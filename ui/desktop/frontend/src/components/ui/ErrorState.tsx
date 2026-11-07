import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'
import { IconAlertTriangle, IconRefresh } from '../common/Icons'
import { Button } from './Button'

export interface ErrorStateProps {
  /** What happened ("Couldn't load teams"). Defaults to a generic message. */
  title?: ReactNode
  /** What to do next. Defaults to "Check that the gateway is running, then try again." */
  description?: ReactNode
  /** Short technical code shown in mono (HTTP status, RPC error code). */
  code?: string
  /** Shows a "Try again" button when provided. */
  onRetry?: () => void
  retrying?: boolean
  className?: string
}

/** Load failure: icon, what happened, what to do, Try again, and a short code. */
export function ErrorState({ title, description, code, onRetry, retrying = false, className }: ErrorStateProps) {
  const { t } = useTranslation('common')
  return (
    <div role="alert" className={cn('flex flex-col items-center justify-center gap-3 px-6 py-12 text-center', className)}>
      <span className="flex size-12 items-center justify-center rounded-lg bg-danger-subtle text-danger">
        <IconAlertTriangle size={20} />
      </span>
      <div className="flex max-w-sm flex-col gap-1">
        <p className="text-ui font-semibold text-ink">{title ?? t('state.errorTitle')}</p>
        <p className="text-caption text-ink-muted">{description ?? t('state.errorHint')}</p>
        {code && <p className="text-code text-ink-muted">{t('state.errorCode', { code })}</p>}
      </div>
      {onRetry && (
        <Button variant="secondary" icon={<IconRefresh size={14} />} onClick={onRetry} loading={retrying}>
          {t('tryAgain')}
        </Button>
      )}
    </div>
  )
}

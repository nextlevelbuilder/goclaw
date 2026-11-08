import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'
import { useToastStore, type Toast as ToastData, type ToastVariant } from '../../stores/toast-store'
import { IconAlertTriangle, IconCheckCircle, IconClose, IconInfo, IconXCircle } from '../common/Icons'

export { toast, useToastStore } from '../../stores/toast-store'
export type { ToastData, ToastVariant }

const variantIcon: Record<ToastVariant, ReactNode> = {
  success: <IconCheckCircle size={16} className="text-success" />,
  destructive: <IconXCircle size={16} className="text-danger" />,
  warning: <IconAlertTriangle size={16} className="text-warning" />,
  default: <IconInfo size={16} className="text-info" />,
}

export function ToastCard({ toast: item, onDismiss }: { toast: ToastData; onDismiss: (id: string) => void }) {
  const { t } = useTranslation('common')
  const urgent = item.variant === 'destructive' || item.variant === 'warning'
  return (
    <div
      role={urgent ? 'alert' : 'status'}
      data-variant={item.variant}
      style={{ animation: 'toast-slide-in 200ms ease-out' }}
      className="pointer-events-auto flex w-full items-start gap-2.5 rounded-lg border border-border-subtle bg-surface-raised py-3 pl-3.5 pr-2 text-ui font-normal text-ink-secondary shadow-popover"
    >
      <span className="mt-0.5 shrink-0">{variantIcon[item.variant]}</span>
      <div className="min-w-0 flex-1 break-words">
        <p className="font-semibold text-ink">{item.title}</p>
        {item.message && <p>{item.message}</p>}
        {item.action && (
          <button
            type="button"
            onClick={() => { item.action?.onClick(); onDismiss(item.id) }}
            className="mt-1 rounded-sm font-medium text-accent-subtle-ink hover:underline"
          >
            {item.action.label}
          </button>
        )}
      </div>
      <button
        type="button"
        aria-label={t('dismiss')}
        onClick={() => onDismiss(item.id)}
        className="flex size-6 shrink-0 items-center justify-center rounded-sm text-ink-muted hover:bg-surface-hover hover:text-ink"
      >
        <IconClose size={12} />
      </button>
    </div>
  )
}

/** Toast stack, bottom-right, newest last, at most three visible. Mount once at the app root. */
export function Toaster({ className }: { className?: string }) {
  const toasts = useToastStore((s) => s.toasts)
  const dismiss = useToastStore((s) => s.dismiss)
  if (toasts.length === 0) return null
  return (
    <div
      className={cn('pointer-events-none fixed bottom-6 right-6 z-[300] flex w-[340px] max-w-[calc(100vw-3rem)] flex-col gap-2', className)}
    >
      {toasts.slice(-3).map((item) => (
        <ToastCard key={item.id} toast={item} onDismiss={dismiss} />
      ))}
    </div>
  )
}

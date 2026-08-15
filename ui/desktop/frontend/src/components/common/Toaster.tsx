import { useToastStore, type Toast } from '../../stores/toast-store'

const styles: Record<Toast['variant'], string> = {
  success: 'border-emerald-500/30 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300',
  destructive: 'border-red-500/30 bg-red-500/10 text-red-700 dark:text-red-300',
  warning: 'border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300',
  default: 'border-border bg-surface-secondary text-text-primary',
}

function ToastIcon({ variant }: { variant: Toast['variant'] }) {
  const cls = 'mt-0.5 h-4 w-4 shrink-0'
  switch (variant) {
    case 'success':
      return (<svg className={cls} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" /><path d="m9 11 3 3L22 4" /></svg>)
    case 'destructive':
      return (<svg className={cls} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10" /><path d="m15 9-6 6" /><path d="m9 9 6 6" /></svg>)
    case 'warning':
      return (<svg className={cls} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"><path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3" /><path d="M12 9v4" /><path d="M12 17h.01" /></svg>)
    default:
      return (<svg className={cls} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10" /><path d="M12 16v-4" /><path d="M12 8h.01" /></svg>)
  }
}

function ToastItem({ toast }: { toast: Toast }) {
  const dismiss = useToastStore((s) => s.dismiss)

  return (
    <div
      style={{ animation: 'toast-slide-in 200ms ease-out' }}
      className={`pointer-events-auto flex w-full items-start gap-3 overflow-hidden rounded-lg border p-4 shadow-lg backdrop-blur-sm ${styles[toast.variant]}`}
      role="alert"
    >
      <ToastIcon variant={toast.variant} />
      <div className="flex-1 min-w-0 space-y-0.5">
        <p className="text-sm font-medium break-words">{toast.title}</p>
        {toast.message && (
          <p className="text-xs opacity-80 break-words">{toast.message}</p>
        )}
      </div>
      <button
        onClick={() => dismiss(toast.id)}
        className="shrink-0 rounded-sm opacity-50 hover:opacity-100 transition-opacity"
      >
        <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <path d="M18 6 6 18" /><path d="m6 6 12 12" />
        </svg>
      </button>
    </div>
  )
}

export function Toaster() {
  const toasts = useToastStore((s) => s.toasts)

  if (toasts.length === 0) return null

  return (
    <div className="fixed bottom-4 right-4 z-[100] flex max-w-sm flex-col-reverse gap-2 pointer-events-none">
      {toasts.slice(-3).map((t) => (
        <ToastItem key={t.id} toast={t} />
      ))}
    </div>
  )
}

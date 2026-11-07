import type { ReactNode } from 'react'
import { cn } from '../../lib/cn'

/** A titled group of settings rows on a raised card. */
export function SettingsCard({ title, children, tone = 'default' }: { title: string; children: ReactNode; tone?: 'default' | 'danger' }) {
  return (
    <section className="flex flex-col gap-2">
      <h2 className={cn('text-ui font-semibold', tone === 'danger' ? 'text-danger' : 'text-ink')}>{title}</h2>
      <div
        className={cn(
          'divide-y divide-border-subtle rounded-lg border bg-surface-raised px-5',
          tone === 'danger' ? 'border-danger' : 'border-border-subtle',
        )}
      >
        {children}
      </div>
    </section>
  )
}

/** One setting: label and hint on the left, the control on the right. */
export function SettingsRow({ label, hint, children, htmlFor }: { label: string; hint?: string; children: ReactNode; htmlFor?: string }) {
  return (
    <div className="flex items-center gap-4 py-3.5">
      <div className="min-w-0 flex-1">
        <label htmlFor={htmlFor} className="block text-ui font-medium text-ink">{label}</label>
        {hint && <p className="text-caption text-ink-muted">{hint}</p>}
      </div>
      <div className="flex min-w-0 shrink-0 items-center gap-2">{children}</div>
    </div>
  )
}

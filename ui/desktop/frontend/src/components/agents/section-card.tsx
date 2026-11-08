import type { ReactNode } from 'react'
import { cn } from '../../lib/cn'

interface SectionCardProps {
  title?: ReactNode
  subtitle?: ReactNode
  children: ReactNode
  className?: string
  tone?: 'default' | 'danger'
}

/** Raised card that groups one block of an agent's settings. */
export function SectionCard({ title, subtitle, children, className, tone = 'default' }: SectionCardProps) {
  return (
    <section
      className={cn(
        'flex flex-col gap-4 rounded-lg border bg-surface-raised p-5',
        tone === 'danger' ? 'border-danger' : 'border-border-subtle',
        className,
      )}
    >
      {title && (
        <header className="flex flex-col gap-0.5">
          <h2 className="text-ui font-semibold text-ink">{title}</h2>
          {subtitle && <p className="text-caption font-normal text-ink-muted">{subtitle}</p>}
        </header>
      )}
      {children}
    </section>
  )
}

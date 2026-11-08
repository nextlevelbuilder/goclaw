import type { ReactNode } from 'react'
import { cn } from '../../lib/cn'

export interface PageHeaderProps {
  /** Page title in title-lg ("Agents"). */
  title: ReactNode
  /** One line under the title ("5 agents · 3 running"). */
  description?: ReactNode
  /** Right-aligned actions: at most one primary Button per view. */
  actions?: ReactNode
  /** Element before the title, e.g. a back IconButton. */
  leading?: ReactNode
  className?: string
}

/** Top of a page: title, description and one primary action slot. */
export function PageHeader({ title, description, actions, leading, className }: PageHeaderProps) {
  return (
    <header className={cn('flex shrink-0 items-center gap-3 border-b border-border-subtle px-6 pt-5 pb-4', className)}>
      {leading}
      <div className="min-w-0 flex-1">
        <h1 className="truncate text-title-lg text-ink">{title}</h1>
        {description && <p className="truncate text-caption text-ink-muted">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </header>
  )
}

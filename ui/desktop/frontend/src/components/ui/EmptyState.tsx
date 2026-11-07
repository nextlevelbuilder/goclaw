import type { ReactNode } from 'react'
import { cn } from '../../lib/cn'
import { IconInbox } from '../common/Icons'

export interface EmptyStateProps {
  /** Short title ("No teams yet"). */
  title: ReactNode
  /** One sentence: what the thing is or what to do. */
  description?: ReactNode
  /** One action, usually a primary Button. */
  action?: ReactNode
  /** 20px icon inside the tile. */
  icon?: ReactNode
  className?: string
}

/** Empty list or screen: tile, one sentence, one action. */
export function EmptyState({ title, description, action, icon, className }: EmptyStateProps) {
  return (
    <div className={cn('flex flex-col items-center justify-center gap-3 px-6 py-12 text-center', className)}>
      <span className="flex size-12 items-center justify-center rounded-lg border border-border-subtle bg-surface-raised text-ink-muted">
        {icon ?? <IconInbox size={20} />}
      </span>
      <div className="flex max-w-sm flex-col gap-1">
        <p className="text-ui font-semibold text-ink">{title}</p>
        {description && <p className="text-caption text-ink-muted">{description}</p>}
      </div>
      {action && <div className="mt-1">{action}</div>}
    </div>
  )
}

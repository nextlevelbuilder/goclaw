import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'

export interface SkeletonProps {
  className?: string
}

/** One placeholder block with a slow shimmer. Size it with className (h-3 w-1/2, size-9 rounded-pill...). */
export function Skeleton({ className }: SkeletonProps) {
  return <span aria-hidden="true" className={cn('skeleton-shimmer block h-3 rounded-sm bg-surface-window', className)} />
}

export interface SkeletonListProps {
  /** Number of placeholder rows. */
  rows?: number
  /** Show a round avatar placeholder at the start of each row. */
  avatar?: boolean
  className?: string
  /** Accessible label; defaults to "Loading". */
  label?: string
}

/** Loading state for lists and tables: rows of shimmer while the header and filters stay real. */
export function SkeletonList({ rows = 5, avatar = true, className, label }: SkeletonListProps) {
  const { t } = useTranslation('common')
  return (
    <div role="status" aria-busy="true" aria-label={label ?? t('state.loading')} className={cn('flex flex-col', className)}>
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="flex min-h-row items-center gap-3 border-b border-border-subtle px-4 py-3 last:border-b-0">
          {avatar && <Skeleton className="size-9 shrink-0 rounded-pill" />}
          <div className="flex flex-1 flex-col gap-2">
            <Skeleton className="w-3/5" />
            <Skeleton className="w-11/12" />
          </div>
        </div>
      ))}
    </div>
  )
}

import type { ComponentPropsWithoutRef } from 'react'
import { cn } from '../../lib/cn'

export type BadgeTone = 'neutral' | 'accent' | 'success' | 'warning' | 'danger' | 'info'

export interface BadgeProps extends ComponentPropsWithoutRef<'span'> {
  tone?: BadgeTone
}

const toneClass: Record<BadgeTone, string> = {
  neutral: 'bg-surface-raised text-ink-secondary border border-border-subtle',
  accent: 'bg-accent-subtle text-accent-subtle-ink',
  success: 'bg-success-subtle text-success',
  warning: 'bg-warning-subtle text-warning',
  danger: 'bg-danger-subtle text-danger',
  info: 'bg-info-subtle text-info',
}

/** Small label or count (tags, edition, unread count). Not for status: use StatusChip. */
export function Badge({ tone = 'neutral', className, ...props }: BadgeProps) {
  return (
    <span
      className={cn(
        'inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-pill px-1.5 text-caption font-medium whitespace-nowrap',
        toneClass[tone],
        className,
      )}
      {...props}
    />
  )
}

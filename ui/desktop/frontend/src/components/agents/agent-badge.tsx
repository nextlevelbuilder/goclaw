import { cn } from '../../lib/cn'
import { IconBot } from '../common/Icons'

interface AgentBadgeProps {
  emoji?: string
  size?: 'sm' | 'md' | 'lg'
  className?: string
}

const sizeClass = { sm: 'size-7 text-ui', md: 'size-9 text-body', lg: 'size-12 text-title' } as const

/** Emoji (or bot icon) tile that identifies an agent in lists and headers. */
export function AgentBadge({ emoji, size = 'md', className }: AgentBadgeProps) {
  return (
    <span
      aria-hidden="true"
      className={cn('flex shrink-0 items-center justify-center rounded-md bg-accent-subtle text-accent-subtle-ink', sizeClass[size], className)}
    >
      {emoji ? <span className="leading-none">{emoji}</span> : <IconBot size={size === 'lg' ? 22 : 16} />}
    </span>
  )
}

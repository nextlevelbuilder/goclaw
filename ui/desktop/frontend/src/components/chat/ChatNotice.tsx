import type { ReactNode } from 'react'
import { cn } from '../../lib/cn'
import { IconAlertTriangle } from '../common/Icons'

export interface ChatNoticeProps {
  tone: 'warning' | 'danger'
  /** What happened, in one short sentence. */
  title: ReactNode
  /** What it means or what to do. */
  body?: ReactNode
  /** Buttons for the recovery actions. */
  actions?: ReactNode
}

/** Inline notice under the transcript: a blocked tool, a cooling-down provider or an interrupted reply. */
export function ChatNotice({ tone, title, body, actions }: ChatNoticeProps) {
  return (
    <div
      role="alert"
      className={cn(
        'flex gap-3 rounded-md border border-border-subtle p-3',
        tone === 'danger' ? 'bg-danger-subtle' : 'bg-warning-subtle',
      )}
    >
      <IconAlertTriangle size={16} className={cn('mt-0.5 shrink-0', tone === 'danger' ? 'text-danger' : 'text-warning')} />
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <p className="text-ui text-ink">
          <span className="font-semibold">{title}</span>
          {body && <span className="ml-1 font-normal text-ink-secondary">{body}</span>}
        </p>
        {actions && <div className="mt-1.5 flex flex-wrap items-center gap-2">{actions}</div>}
      </div>
    </div>
  )
}

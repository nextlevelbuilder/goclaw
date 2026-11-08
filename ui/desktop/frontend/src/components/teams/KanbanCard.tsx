import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { IconBlocked, IconChat, IconPaperclip } from '../common/Icons'
import { cn } from '../../lib/cn'
import { PRIORITY_BADGE, isTaskLocked, TERMINAL_STATUSES } from '../../types/team'
import type { TeamTaskData } from '../../types/team'

interface KanbanCardProps {
  task: TeamTaskData
  ownerName?: string
  ownerEmoji?: string
  onClick: () => void
}

/** One task on the board: identifier, priority, subject, owner and progress. */
export function KanbanCard({ task, ownerName, ownerEmoji, onClick }: KanbanCardProps) {
  const { t } = useTranslation('teams')
  const locked = isTaskLocked(task)
  const blocked = task.status === 'blocked'
  const prio = PRIORITY_BADGE[task.priority] ?? PRIORITY_BADGE[3]
  const hasBlockers = !!task.blocked_by && task.blocked_by.length > 0
  const isTerminal = TERMINAL_STATUSES.has(task.status)

  return (
    <motion.button
      type="button"
      layoutId={task.id}
      layout
      initial={false}
      transition={{ type: 'spring', stiffness: 350, damping: 30 }}
      onClick={onClick}
      className={cn(
        'w-full cursor-pointer rounded-md border bg-surface-raised p-3 text-left transition-colors hover:bg-surface-hover',
        'focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-focus-ring',
        locked ? 'border-success' : blocked ? 'border-warning' : 'border-border-subtle',
      )}
    >
      <div className="mb-1 flex items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <span className="font-mono text-caption text-ink-muted">{task.identifier || `#${task.task_number ?? ''}`}</span>
          <span className={cn('rounded-sm px-1.5 font-mono text-caption', prio.cls)}>{prio.label}</span>
        </div>
        {locked && (
          <span className="flex items-center gap-1 text-caption text-success">
            <span aria-hidden="true" className="size-1.5 animate-pulse rounded-pill bg-success" />
            {t('board.running')}
          </span>
        )}
      </div>

      <p className="line-clamp-2 text-ui font-medium text-ink">{task.subject}</p>

      {hasBlockers && (
        <p className="mt-1 flex items-center gap-1 text-caption text-warning">
          <IconBlocked size={12} className="shrink-0" />
          <span className="truncate">{task.blocked_by!.map((id) => id.slice(0, 8)).join(', ')}</span>
        </p>
      )}

      <div className="mt-2 flex items-center gap-2">
        {ownerEmoji && <span aria-hidden="true" className="text-body leading-none">{ownerEmoji}</span>}
        <span className="flex-1 truncate text-caption text-ink-muted">
          {ownerName || task.owner_agent_key || t('board.unassigned')}
        </span>
        {(task.comment_count ?? 0) > 0 && (
          <span className="flex shrink-0 items-center gap-0.5 text-caption text-ink-muted">
            <IconChat size={12} />
            {task.comment_count}
          </span>
        )}
        {(task.attachment_count ?? 0) > 0 && (
          <span className="flex shrink-0 items-center gap-0.5 text-caption text-ink-muted">
            <IconPaperclip size={12} />
            {task.attachment_count}
          </span>
        )}
      </div>

      {task.progress_percent != null && task.progress_percent > 0 && !isTerminal && (
        <div className="mt-2 flex items-center gap-2">
          <div className="h-1.5 flex-1 overflow-hidden rounded-pill bg-surface-hover">
            <div className="h-full rounded-pill bg-accent transition-all duration-500" style={{ width: `${Math.min(task.progress_percent, 100)}%` }} />
          </div>
          <span className="text-caption text-ink-muted">{task.progress_percent}%</span>
        </div>
      )}
    </motion.button>
  )
}

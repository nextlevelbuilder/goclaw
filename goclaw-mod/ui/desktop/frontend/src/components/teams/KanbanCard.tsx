import { motion } from 'framer-motion'
import { IconBlocked, IconChat, IconPaperclip } from '../common/Icons'
import { isTaskLocked, TERMINAL_STATUSES } from '../../types/team'
import type { TeamTaskData } from '../../types/team'

/** Priority: plain text color, matching web kanban-card.tsx */
const PRIORITY_STYLE: Record<number, { label: string; color: string }> = {
  0: { label: 'P-0', color: 'text-slate-400' },
  1: { label: 'P-1', color: 'text-blue-500' },
  2: { label: 'P-2', color: 'text-amber-500' },
  3: { label: 'P-3', color: 'text-red-500' },
}


interface KanbanCardProps {
  task: TeamTaskData
  ownerName?: string
  ownerEmoji?: string
  onClick: () => void
}

export function KanbanCard({ task, ownerName, ownerEmoji, onClick }: KanbanCardProps) {
  const locked = isTaskLocked(task)
  const blocked = task.status === 'blocked'
  const prio = PRIORITY_STYLE[task.priority] ?? PRIORITY_STYLE[0]
  const hasBlockers = task.blocked_by && task.blocked_by.length > 0
  const isTerminal = TERMINAL_STATUSES.has(task.status)

  return (
    <motion.button
      layoutId={task.id}
      layout
      initial={false}
      transition={{ type: 'spring', stiffness: 350, damping: 30 }}
      onClick={onClick}
      className={[
        'w-full text-left rounded-lg border bg-surface-primary p-3 transition-colors hover:bg-accent/5 cursor-pointer group',
        locked ? 'border-l-2 border-l-green-500' : blocked ? 'border-l-2 border-l-amber-500' : 'border-border',
      ].join(' ')}
    >
      {/* Top row: identifier + priority + running */}
      <div className="flex items-center justify-between mb-1">
        <div className="flex items-center gap-1.5">
          <span className="font-mono text-[10px] text-text-muted">
            {task.identifier || `#${task.task_number ?? ''}`}
          </span>
          <span className={`font-mono text-[10px] font-medium ${prio.color}`}>
            {prio.label}
          </span>
        </div>
        {locked && (
          <span className="flex items-center gap-1 text-[10px] text-green-600 dark:text-green-400">
            <span className="h-1.5 w-1.5 animate-pulse rounded-full bg-green-500" />
            Running
          </span>
        )}
      </div>

      {/* Subject */}
      <p className="text-xs font-medium text-text-primary leading-snug line-clamp-2">
        {task.subject}
      </p>

      {/* Blocked indicator */}
      {hasBlockers && (
        <p className="mt-1 flex items-center gap-1 text-[10px] text-amber-600 dark:text-amber-400">
          <IconBlocked size={10} className="shrink-0" />
          <span className="truncate">
            {task.blocked_by!.map((id) => id.slice(0, 8)).join(', ')}
          </span>
        </p>
      )}

      {/* Bottom: owner + counts */}
      <div className="mt-2 flex items-center gap-1.5">
        {ownerEmoji && <span className="text-sm leading-none">{ownerEmoji}</span>}
        <span className="truncate text-xs text-text-muted flex-1">
          {ownerName || task.owner_agent_key || 'Unassigned'}
        </span>
        {(task.comment_count ?? 0) > 0 && (
          <span className="flex items-center gap-0.5 text-[10px] text-text-muted shrink-0">
            <IconChat size={10} />
            {task.comment_count}
          </span>
        )}
        {(task.attachment_count ?? 0) > 0 && (
          <span className="flex items-center gap-0.5 text-[10px] text-text-muted shrink-0">
            <IconPaperclip size={10} />
            {task.attachment_count}
          </span>
        )}
      </div>

      {/* Progress bar */}
      {task.progress_percent != null && task.progress_percent > 0 && !isTerminal && (
        <div className="mt-2 flex items-center gap-1.5">
          <div className="h-1.5 flex-1 rounded-full bg-surface-tertiary overflow-hidden">
            <div className="h-full rounded-full bg-accent transition-all duration-500" style={{ width: `${Math.min(task.progress_percent, 100)}%` }} />
          </div>
          <span className="text-[10px] text-text-muted">{task.progress_percent}%</span>
        </div>
      )}
    </motion.button>
  )
}

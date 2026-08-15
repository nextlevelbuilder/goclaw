import { AnimatePresence, LayoutGroup } from 'framer-motion'
import type { TaskStatus, TeamTaskData, TeamMemberData } from '../../types/team'
import { STATUS_COLORS } from '../../types/team'
import { KanbanCard } from './KanbanCard'

const STATUS_LABELS: Record<TaskStatus, string> = {
  pending: 'Pending',
  blocked: 'Blocked',
  in_progress: 'In Progress',
  completed: 'Completed',
  failed: 'Failed',
  cancelled: 'Cancelled',
}

interface KanbanColumnProps {
  status: TaskStatus
  tasks: TeamTaskData[]
  members: TeamMemberData[]
  onTaskClick: (task: TeamTaskData) => void
}

export function KanbanColumn({ status, tasks, members, onTaskClick }: KanbanColumnProps) {
  const memberMap = new Map(members.map((m) => [m.agent_id, m]))

  return (
    <div className="flex w-[260px] shrink-0 flex-col rounded-xl border border-border bg-surface-secondary/50 max-h-full self-start">
      {/* Column header */}
      <div className="flex items-center gap-2 px-3 py-2.5">
        <div className={`w-2.5 h-2.5 rounded-full ${STATUS_COLORS[status]}`} />
        <span className="text-xs font-medium text-text-primary capitalize">{STATUS_LABELS[status]}</span>
        <span className="ml-auto text-[10px] text-text-muted bg-surface-tertiary px-1.5 py-0.5 rounded-full">
          {tasks.length}
        </span>
      </div>

      {/* Card list */}
      <div className="flex flex-1 flex-col gap-2 overflow-y-auto overscroll-contain px-2 pb-2">
        {tasks.length === 0 ? (
          <div className="py-3 text-center text-[10px] text-text-muted opacity-50">
            No tasks
          </div>
        ) : (
          <LayoutGroup>
            <AnimatePresence mode="popLayout">
              {tasks.map((task) => {
                const member = task.owner_agent_id ? memberMap.get(task.owner_agent_id) : undefined
                return (
                  <KanbanCard
                    key={task.id}
                    task={task}
                    ownerName={member?.display_name || member?.agent_key}
                    ownerEmoji={member?.emoji}
                    onClick={() => onTaskClick(task)}
                  />
                )
              })}
            </AnimatePresence>
          </LayoutGroup>
        )}
      </div>
    </div>
  )
}

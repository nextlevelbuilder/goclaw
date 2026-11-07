import { AnimatePresence, LayoutGroup } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import type { TaskStatus, TeamTaskData, TeamMemberData } from '../../types/team'
import { STATUS_COLORS } from '../../types/team'
import { KanbanCard } from './KanbanCard'

interface KanbanColumnProps {
  status: TaskStatus
  tasks: TeamTaskData[]
  members: TeamMemberData[]
  onTaskClick: (task: TeamTaskData) => void
}

/** One status column of the board. */
export function KanbanColumn({ status, tasks, members, onTaskClick }: KanbanColumnProps) {
  const { t } = useTranslation('teams')
  const memberMap = new Map(members.map((m) => [m.agent_id, m]))
  const label = t(`board.status.${status}`)

  return (
    <section aria-label={label} className="flex max-h-full w-[260px] shrink-0 flex-col self-start rounded-card border border-border-subtle bg-surface-secondary">
      <header className="flex items-center gap-2 px-3 py-2.5">
        <span aria-hidden="true" className={`size-2.5 rounded-pill ${STATUS_COLORS[status]}`} />
        <h2 className="text-ui font-medium text-ink">{label}</h2>
        <span className="ml-auto rounded-pill bg-surface-tertiary px-1.5 text-caption text-ink-secondary">{tasks.length}</span>
      </header>

      <div className="flex flex-1 flex-col gap-2 overflow-y-auto overscroll-contain px-2 pb-2">
        {tasks.length === 0 ? (
          <p className="py-3 text-center text-caption text-ink-muted">{t('board.columnEmpty')}</p>
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
    </section>
  )
}

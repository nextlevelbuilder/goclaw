import { useTranslation } from 'react-i18next'
import { TERMINAL_STATUSES } from '../../types/team'
import type { TeamTaskData, TeamMemberData } from '../../types/team'

interface TeamTaskListViewProps {
  tasks: TeamTaskData[]
  members: TeamMemberData[]
  loading: boolean
  selected: Set<string>
  onSelectChange: (next: Set<string>) => void
  onTaskClick: (task: TeamTaskData) => void
  onBulkDelete: () => void
}

export function TeamTaskListView({
  tasks, members, loading, selected, onSelectChange, onTaskClick, onBulkDelete,
}: TeamTaskListViewProps) {
  const { t } = useTranslation('teams')

  return (
    <div className="flex-1 overflow-auto p-3">
      {selected.size > 0 && (
        <div className="flex items-center gap-2 mb-2 px-2">
          <span className="text-xs text-text-muted">{selected.size} selected</span>
          <button onClick={onBulkDelete} className="text-xs text-error hover:underline cursor-pointer">
            {t('deleteSelected', 'Delete selected')}
          </button>
        </div>
      )}
      <div className="overflow-x-auto">
        <table className="w-full text-xs min-w-[600px]">
          <thead>
            <tr className="text-text-muted text-left border-b border-border">
              <th className="pb-2 pr-2 w-8">
                <input
                  type="checkbox"
                  checked={selected.size > 0 && selected.size === tasks.filter((t) => TERMINAL_STATUSES.has(t.status)).length}
                  onChange={(e) => {
                    if (e.target.checked) {
                      onSelectChange(new Set(tasks.filter((t) => TERMINAL_STATUSES.has(t.status)).map((t) => t.id)))
                    } else {
                      onSelectChange(new Set())
                    }
                  }}
                />
              </th>
              <th className="pb-2 pr-2 font-medium">{t('id', 'ID')}</th>
              <th className="pb-2 pr-2 font-medium">{t('subject', 'Subject')}</th>
              <th className="pb-2 pr-2 font-medium">{t('status', 'Status')}</th>
              <th className="pb-2 pr-2 font-medium">{t('owner', 'Owner')}</th>
              <th className="pb-2 font-medium">{t('priority', 'Priority')}</th>
            </tr>
          </thead>
          <tbody>
            {tasks.map((task) => {
              const member = task.owner_agent_id ? members.find((m) => m.agent_id === task.owner_agent_id) : undefined
              const canSelect = TERMINAL_STATUSES.has(task.status)
              return (
                <tr
                  key={task.id}
                  onClick={() => onTaskClick(task)}
                  className="border-b border-border/50 hover:bg-surface-tertiary/50 cursor-pointer transition-colors"
                >
                  <td className="py-2 pr-2" onClick={(e) => e.stopPropagation()}>
                    <input
                      type="checkbox"
                      disabled={!canSelect}
                      checked={selected.has(task.id)}
                      onChange={(e) => {
                        const next = new Set(selected)
                        e.target.checked ? next.add(task.id) : next.delete(task.id)
                        onSelectChange(next)
                      }}
                    />
                  </td>
                  <td className="py-2 pr-2 font-mono text-text-muted">{task.identifier || task.task_number}</td>
                  <td className="py-2 pr-2 text-text-primary truncate max-w-[250px]">{task.subject}</td>
                  <td className="py-2 pr-2">
                    <span className="capitalize text-text-secondary">{task.status.replace('_', ' ')}</span>
                  </td>
                  <td className="py-2 pr-2 text-text-secondary">{member?.display_name || task.owner_agent_key || '—'}</td>
                  <td className="py-2 text-text-secondary">P-{task.priority}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      {tasks.length === 0 && !loading && (
        <div className="text-center py-8 text-text-muted text-xs">{t('noTasks', 'No tasks yet')}</div>
      )}
    </div>
  )
}

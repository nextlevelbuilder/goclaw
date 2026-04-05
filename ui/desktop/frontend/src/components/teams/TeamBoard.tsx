import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useUiStore } from '../../stores/ui-store'
import { useTeamTasks } from '../../hooks/use-team-tasks'
import { RefreshButton } from '../common/RefreshButton'
import { IconChevronLeft, IconGear, IconChevronDown, IconPlus, IconChat } from '../common/Icons'
import { KanbanColumn } from './KanbanColumn'
import { TaskDetailModal } from './TaskDetailModal'
import { TeamSettingsModal } from './TeamSettingsModal'
import { KANBAN_STATUSES, TERMINAL_STATUSES, groupByStatus } from '../../types/team'
import type { TeamTaskData } from '../../types/team'

type ViewMode = 'kanban' | 'list'
const FILTER_OPTIONS = [
  { value: '', label: 'allStatuses' },
  { value: 'active', label: 'active' },
  { value: 'completed', label: 'completed' },
] as const

export function TeamBoard() {
  const { t } = useTranslation('teams')
  const activeTeamId = useUiStore((s) => s.activeTeamId)
  const closeSettings = useUiStore((s) => s.closeSettings)
  const { teams, tasks, members, loading, fetchTeams, fetchTasks, fetchTaskDetail, assignTask, deleteTask, deleteBulk } = useTeamTasks()

  const [viewMode, setViewMode] = useState<ViewMode>('kanban')
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [filterOpen, setFilterOpen] = useState(false)
  const [selectedTask, setSelectedTask] = useState<TeamTaskData | null>(null)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [infoOpen, setInfoOpen] = useState(false)

  const team = teams.find((t) => t.id === activeTeamId)

  useEffect(() => { fetchTeams() }, [fetchTeams])

  useEffect(() => {
    if (activeTeamId) fetchTasks(activeTeamId, statusFilter || undefined)
  }, [activeTeamId, statusFilter, fetchTasks])

  const grouped = useMemo(() => groupByStatus(tasks), [tasks])

  const handleRefresh = () => {
    if (activeTeamId) fetchTasks(activeTeamId, statusFilter || undefined)
  }

  // Keep selected task in sync with tasks
  useEffect(() => {
    if (selectedTask) {
      const updated = tasks.find((t) => t.id === selectedTask.id)
      if (updated) setSelectedTask(updated)
    }
  }, [tasks, selectedTask])

  const handleBulkDelete = async () => {
    if (selected.size === 0) return
    await deleteBulk(Array.from(selected))
    setSelected(new Set())
  }

  const currentFilterLabel = FILTER_OPTIONS.find((o) => o.value === statusFilter)?.label || 'allStatuses'

  if (!activeTeamId) {
    return (
      <div className="flex-1 flex items-center justify-center text-text-muted text-sm">
        {t('selectTeam', 'Select a team from the sidebar')}
      </div>
    )
  }

  return (
    <div className="flex-1 flex flex-col overflow-hidden">
      {/* Header */}
      <div className="flex items-center gap-3 px-4 py-3 border-b border-border shrink-0">
        <button onClick={closeSettings} className="text-text-muted hover:text-text-primary cursor-pointer" title="Back to chat">
          <IconChevronLeft size={16} />
        </button>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <h2 className="text-sm font-semibold text-text-primary truncate">
              {team?.name || t('team', 'Team')}
            </h2>
            {/* Gear — team settings */}
            <button
              onClick={() => setSettingsOpen(true)}
              className="text-text-muted hover:text-text-primary cursor-pointer p-1 rounded hover:bg-surface-tertiary"
              title={t('teamSettings', 'Team Settings')}
            >
              <IconGear />
            </button>
          </div>
        </div>

        {/* View toggle */}
        <div className="flex items-center bg-surface-tertiary rounded-lg p-0.5">
          <button
            onClick={() => setViewMode('kanban')}
            className={`text-[10px] px-2 py-1 rounded cursor-pointer ${viewMode === 'kanban' ? 'bg-surface-primary text-text-primary shadow-sm' : 'text-text-muted'}`}
          >
            {t('kanban', 'Kanban')}
          </button>
          <button
            onClick={() => setViewMode('list')}
            className={`text-[10px] px-2 py-1 rounded cursor-pointer ${viewMode === 'list' ? 'bg-surface-primary text-text-primary shadow-sm' : 'text-text-muted'}`}
          >
            {t('list', 'List')}
          </button>
        </div>

        {/* Custom status filter dropdown */}
        <div className="relative">
          <button
            onClick={() => setFilterOpen((v) => !v)}
            className="flex items-center gap-1.5 text-[11px] bg-surface-tertiary border border-border rounded-lg px-2.5 py-1.5 text-text-secondary hover:border-accent/30 transition-colors cursor-pointer"
          >
            <span>{t(currentFilterLabel)}</span>
            <IconChevronDown size={10} />
          </button>
          {filterOpen && (
            <>
              <div className="fixed inset-0 z-40" onClick={() => setFilterOpen(false)} />
              <div className="absolute right-0 top-full mt-1 z-50 bg-surface-primary border border-border rounded-lg shadow-lg py-1 min-w-[120px]">
                {FILTER_OPTIONS.map((opt) => (
                  <button
                    key={opt.value}
                    onClick={() => { setStatusFilter(opt.value); setFilterOpen(false) }}
                    className={[
                      'w-full text-left px-3 py-1.5 text-[11px] transition-colors cursor-pointer',
                      statusFilter === opt.value ? 'text-accent bg-accent/10' : 'text-text-secondary hover:bg-surface-tertiary',
                    ].join(' ')}
                  >
                    {t(opt.label)}
                  </button>
                ))}
              </div>
            </>
          )}
        </div>

        <RefreshButton onRefresh={async () => { handleRefresh() }} />

        {/* "+" info button */}
        <button
          onClick={() => setInfoOpen(true)}
          className="flex items-center justify-center w-7 h-7 rounded-lg bg-accent/10 text-accent hover:bg-accent/20 cursor-pointer transition-colors"
          title={t('createTask', 'Create Task')}
        >
          <IconPlus />
        </button>
      </div>

      {/* Kanban view */}
      {viewMode === 'kanban' ? (
        <div className="flex-1 overflow-x-auto overflow-y-hidden p-3">
          <div className="flex gap-3 h-full">
            {KANBAN_STATUSES.map((status) => (
              <KanbanColumn
                key={status}
                status={status}
                tasks={grouped.get(status) ?? []}
                members={members}
                onTaskClick={setSelectedTask}
              />
            ))}
          </div>
        </div>
      ) : (
        /* List view */
        <div className="flex-1 overflow-auto p-3">
          {selected.size > 0 && (
            <div className="flex items-center gap-2 mb-2 px-2">
              <span className="text-xs text-text-muted">{selected.size} selected</span>
              <button onClick={handleBulkDelete} className="text-xs text-error hover:underline cursor-pointer">
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
                          setSelected(new Set(tasks.filter((t) => TERMINAL_STATUSES.has(t.status)).map((t) => t.id)))
                        } else {
                          setSelected(new Set())
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
                      onClick={() => setSelectedTask(task)}
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
                            setSelected(next)
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
      )}

      {/* Task detail modal */}
      {selectedTask && (
        <TaskDetailModal
          task={selectedTask}
          members={members}
          onClose={() => setSelectedTask(null)}
          onAssign={assignTask}
          onDelete={deleteTask}
          onFetchDetail={fetchTaskDetail}
        />
      )}

      {/* Team settings modal */}
      {settingsOpen && activeTeamId && (
        <TeamSettingsModal
          teamId={activeTeamId}
          onClose={() => setSettingsOpen(false)}
          onSaved={() => { fetchTeams(); if (activeTeamId) fetchTasks(activeTeamId, statusFilter || undefined) }}
        />
      )}

      {/* Task create info modal */}
      {infoOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={() => setInfoOpen(false)}>
          <div onClick={(e) => e.stopPropagation()} className="bg-surface-primary border border-border rounded-xl shadow-xl max-w-sm mx-4 p-6 text-center space-y-3">
            <div className="mx-auto w-10 h-10 rounded-full bg-accent/10 flex items-center justify-center">
              <IconChat size={20} className="text-accent" />
            </div>
            <h3 className="text-sm font-semibold text-text-primary">{t('taskCreateInfo.title', 'How tasks are created')}</h3>
            <p className="text-xs text-text-secondary leading-relaxed">
              {t('taskCreateInfo.body', 'Tasks are created by chatting with the team leader agent. Start a conversation with {{leader}} to create and manage tasks.', {
                leader: team?.lead_display_name || team?.lead_agent_key || 'the leader',
              })}
            </p>
            <button
              onClick={() => setInfoOpen(false)}
              className="mt-2 px-4 py-1.5 text-xs font-medium bg-surface-tertiary text-text-primary rounded-lg hover:bg-surface-tertiary/80 cursor-pointer"
            >
              {t('settings.ok', 'OK')}
            </button>
          </div>
        </div>
      )}

    </div>
  )
}

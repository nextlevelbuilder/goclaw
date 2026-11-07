import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useUiStore } from '../../stores/ui-store'
import { useTeamTasks } from '../../hooks/use-team-tasks'
import { IconChat, IconChevronLeft, IconGear, IconPlus, IconRefresh } from '../common/Icons'
import { useGatewayStatus } from '../layout/use-gateway-status'
import { cn } from '../../lib/cn'
import {
  Button, ConfirmDialog, Dialog, DialogFooter, EmptyState, ErrorState, IconButton, PageHeader, SegmentedControl, Select,
  SkeletonList, toast,
} from '../ui'
import { KanbanColumn } from './KanbanColumn'
import { TaskDetailModal } from './TaskDetailModal'
import { TeamSettingsModal } from './TeamSettingsModal'
import { TeamTaskListView } from './team-task-list-view'
import { KANBAN_STATUSES, groupByStatus } from '../../types/team'
import type { TeamTaskData } from '../../types/team'

type ViewMode = 'kanban' | 'list'

/** Task board of one team: kanban or list, status filter, task detail and team settings. */
export function TeamBoard() {
  const { t } = useTranslation('teams')
  const { disconnected } = useGatewayStatus()
  const activeTeamId = useUiStore((s) => s.activeTeamId)
  const setActiveView = useUiStore((s) => s.setActiveView)
  const { teams, tasks, members, loading, error, fetchTeams, fetchTasks, fetchTaskDetail, assignTask, deleteTask, deleteBulk } = useTeamTasks()

  const [viewMode, setViewMode] = useState<ViewMode>('kanban')
  const [statusFilter, setStatusFilter] = useState('')
  const [selectedTask, setSelectedTask] = useState<TeamTaskData | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const [bulkIds, setBulkIds] = useState<string[] | null>(null)
  const [bulkBusy, setBulkBusy] = useState(false)
  const [settingsOpen, setSettingsOpen] = useState(false)
  const [infoOpen, setInfoOpen] = useState(false)
  const [refreshing, setRefreshing] = useState(false)

  const team = teams.find((x) => x.id === activeTeamId)
  const leader = team?.lead_display_name || team?.lead_agent_key || ''

  const reload = useCallback(async () => {
    if (activeTeamId) await fetchTasks(activeTeamId, statusFilter || undefined)
  }, [activeTeamId, statusFilter, fetchTasks])

  useEffect(() => { void fetchTeams() }, [fetchTeams])
  useEffect(() => { void reload() }, [reload])

  const grouped = useMemo(() => groupByStatus(tasks), [tasks])

  // Keep the open task in sync with live updates
  useEffect(() => {
    if (!selectedTask) return
    const updated = tasks.find((x) => x.id === selectedTask.id)
    if (updated && updated !== selectedTask) setSelectedTask(updated)
  }, [tasks, selectedTask])

  const refresh = async () => {
    setRefreshing(true)
    try { await reload() } finally { setRefreshing(false) }
  }

  const handleAssign = async (taskId: string, agentKey: string) => {
    try {
      const res = await assignTask(taskId, agentKey)
      toast.success(t('board.toast.assigned'))
      return res
    } catch (err) {
      toast.error(t('board.toast.assignFailed'), err instanceof Error ? err.message : undefined)
      throw err
    }
  }

  const handleDelete = async (taskId: string) => {
    try {
      await deleteTask(taskId)
      toast.success(t('board.toast.deleted'))
    } catch (err) {
      toast.error(t('board.toast.deleteFailed'), err instanceof Error ? err.message : undefined)
      throw err
    }
  }

  const confirmBulkDelete = async () => {
    if (!bulkIds) return
    setBulkBusy(true)
    try {
      await deleteBulk(bulkIds)
      toast.success(t('board.toast.bulkDeleted', { count: bulkIds.length }))
      setSelected([])
      setBulkIds(null)
    } catch (err) {
      toast.error(t('board.toast.bulkDeleteFailed'), err instanceof Error ? err.message : undefined)
    } finally {
      setBulkBusy(false)
    }
  }

  const goBack = () => setActiveView('teams')

  if (!activeTeamId) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <PageHeader title={t('title')} />
        <EmptyState title={t('board.noTeam')} action={<Button variant="secondary" onClick={goBack}>{t('board.back')}</Button>} />
      </div>
    )
  }

  const filterOptions = [
    { value: '', label: t('allStatuses') },
    { value: 'active', label: t('active') },
    { value: 'completed', label: t('completed') },
  ]

  const header = (
    <PageHeader
      leading={<IconButton aria-label={t('board.back')} icon={<IconChevronLeft size={16} />} onClick={goBack} />}
      title={team?.name || t('team')}
      description={leader ? `${t('leadAgent')}: ${leader}` : undefined}
      actions={(
        <>
          <SegmentedControl<ViewMode>
            value={viewMode}
            onValueChange={setViewMode}
            aria-label={t('board.viewLabel')}
            options={[
              { value: 'kanban', label: t('kanban') },
              { value: 'list', label: t('list') },
            ]}
          />
          <div className="w-36">
            <Select aria-label={t('board.filterLabel')} value={statusFilter} onChange={(e) => setStatusFilter(e.target.value)} options={filterOptions} />
          </div>
          <IconButton aria-label={t('board.refresh')} icon={<IconRefresh size={16} />} onClick={() => void refresh()} loading={refreshing} />
          <IconButton aria-label={t('board.settings')} icon={<IconGear size={16} />} onClick={() => setSettingsOpen(true)} />
          <Button variant="primary" icon={<IconPlus size={16} />} onClick={() => setInfoOpen(true)}>
            {t('board.newTask')}
          </Button>
        </>
      )}
    />
  )

  let body
  if (error && tasks.length === 0) {
    body = (
      <ErrorState
        title={t('board.errorTitle')}
        description={t('board.errorDescription')}
        onRetry={() => void refresh()}
        retrying={refreshing}
      />
    )
  } else if (loading) {
    body = <SkeletonList rows={4} label={t('board.loadingTasks')} />
  } else if (viewMode === 'kanban') {
    body = (
      <div className="flex h-full gap-3 overflow-x-auto overflow-y-hidden">
        {KANBAN_STATUSES.map((status) => (
          <KanbanColumn key={status} status={status} tasks={grouped.get(status) ?? []} members={members} onTaskClick={setSelectedTask} />
        ))}
      </div>
    )
  } else {
    body = (
      <TeamTaskListView
        tasks={tasks}
        members={members}
        selected={selected}
        onSelectChange={setSelected}
        onTaskClick={setSelectedTask}
        onBulkDelete={setBulkIds}
        readOnly={disconnected}
      />
    )
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {header}
      <div className={cn('min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-4', disconnected && 'opacity-60')}>{body}</div>

      {selectedTask && (
        <TaskDetailModal
          task={selectedTask}
          members={members}
          onClose={() => setSelectedTask(null)}
          onAssign={handleAssign}
          onDelete={handleDelete}
          onFetchDetail={fetchTaskDetail}
          readOnly={disconnected}
        />
      )}

      {settingsOpen && (
        <TeamSettingsModal
          teamId={activeTeamId}
          onClose={() => setSettingsOpen(false)}
          onSaved={() => { void fetchTeams(); void reload() }}
          onDeleted={() => { setSettingsOpen(false); goBack() }}
        />
      )}

      <Dialog
        open={infoOpen}
        onOpenChange={setInfoOpen}
        title={t('taskCreateInfo.title')}
        size="sm"
        footer={<DialogFooter><Button variant="primary" onClick={() => setInfoOpen(false)}>{t('settings.ok')}</Button></DialogFooter>}
      >
        <div className="flex flex-col items-center gap-3 text-center">
          <span aria-hidden="true" className="flex size-10 items-center justify-center rounded-pill bg-accent-subtle text-accent-subtle-ink">
            <IconChat size={20} />
          </span>
          <p className="text-ui text-ink-secondary">{t('taskCreateInfo.body', { leader: leader || t('team') })}</p>
        </div>
      </Dialog>

      <ConfirmDialog
        open={bulkIds !== null}
        onOpenChange={(o) => { if (!o && !bulkBusy) setBulkIds(null) }}
        title={t('board.deleteBulkTitle', { count: bulkIds?.length ?? 0 })}
        description={t('board.deleteBulkBody')}
        confirmLabel={t('delete')}
        cancelLabel={t('cancel')}
        variant="destructive"
        loading={bulkBusy}
        onConfirm={() => void confirmBulkDelete()}
      />
    </div>
  )
}

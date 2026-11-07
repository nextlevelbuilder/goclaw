import { useTranslation } from 'react-i18next'
import { IconTrash } from '../common/Icons'
import { Button, DataTable, EmptyState, StatusChip, type DataTableColumn } from '../ui'
import { PRIORITY_BADGE, STATUS_TONE, TERMINAL_STATUSES } from '../../types/team'
import { cn } from '../../lib/cn'
import type { TeamTaskData, TeamMemberData } from '../../types/team'

interface TeamTaskListViewProps {
  tasks: TeamTaskData[]
  members: TeamMemberData[]
  selected: string[]
  onSelectChange: (ids: string[]) => void
  onTaskClick: (task: TeamTaskData) => void
  onBulkDelete: (ids: string[]) => void
  readOnly?: boolean
}

/** Tasks as a table. Only finished tasks can be deleted, so bulk delete acts on those. */
export function TeamTaskListView({
  tasks, members, selected, onSelectChange, onTaskClick, onBulkDelete, readOnly = false,
}: TeamTaskListViewProps) {
  const { t } = useTranslation('teams')
  const memberById = new Map(members.map((m) => [m.agent_id, m]))
  const deletable = new Set(tasks.filter((x) => TERMINAL_STATUSES.has(x.status)).map((x) => x.id))

  const columns: DataTableColumn<TeamTaskData>[] = [
    {
      id: 'id',
      header: t('id'),
      className: 'w-24',
      cell: (task) => <span className="font-mono text-caption text-ink-muted">{task.identifier || task.task_number}</span>,
    },
    {
      id: 'subject',
      header: t('subject'),
      className: 'min-w-[220px]',
      cell: (task) => <span className="block max-w-md truncate text-ui font-medium text-ink">{task.subject}</span>,
    },
    {
      id: 'status',
      header: t('status'),
      className: 'w-40',
      cell: (task) => <StatusChip tone={STATUS_TONE[task.status]} label={t(`board.status.${task.status}`)} />,
    },
    {
      id: 'owner',
      header: t('owner'),
      className: 'w-44',
      cell: (task) => {
        const m = task.owner_agent_id ? memberById.get(task.owner_agent_id) : undefined
        return <span className="block truncate text-ui text-ink-secondary">{m?.display_name || task.owner_agent_key || '—'}</span>
      },
    },
    {
      id: 'priority',
      header: t('priority'),
      className: 'w-24',
      cell: (task) => {
        const prio = PRIORITY_BADGE[task.priority] ?? PRIORITY_BADGE[3]
        return <span className={cn('rounded-sm px-1.5 font-mono text-caption', prio.cls)}>{prio.label}</span>
      },
    },
  ]

  return (
    <DataTable
      aria-label={t('title')}
      columns={columns}
      rows={tasks}
      getRowId={(x) => x.id}
      onRowClick={onTaskClick}
      selectable={!readOnly}
      selectedIds={selected}
      onSelectionChange={onSelectChange}
      empty={<EmptyState title={t('board.listEmptyTitle')} description={t('board.listEmptyBody')} />}
      bulkActions={(ids) => {
        const doomed = ids.filter((id) => deletable.has(id))
        return (
          <Button
            variant="danger"
            size="sm"
            icon={<IconTrash size={14} />}
            disabled={doomed.length === 0}
            onClick={() => onBulkDelete(doomed)}
          >
            {t('deleteSelected')}
          </Button>
        )
      }}
    />
  )
}

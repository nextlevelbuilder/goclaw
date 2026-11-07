import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { DataTable, Menu, StatusChip, Switch, type DataTableColumn, type MenuEntry } from '../ui'
import { IconChat, IconRefresh, IconTrash } from '../common/Icons'
import { AgentBadge } from './agent-badge'
import { agentDisplayName, agentEmoji, agentStatusView, agentSummary, isAgentEnabled } from './agent-display'
import type { AgentData } from '../../types/agent'

interface AgentTableProps {
  agents: AgentData[]
  selectedIds: string[]
  onSelectionChange: (ids: string[]) => void
  bulkActions: (ids: string[]) => ReactNode
  empty?: ReactNode
  /** Disables writes while the gateway is unreachable. */
  readOnly?: boolean
  onOpen: (agent: AgentData) => void
  onChat: (agent: AgentData) => void
  onToggle: (agent: AgentData, enabled: boolean) => void
  onResummon: (agent: AgentData) => void
  onDelete: (agent: AgentData) => void
}

/** Agents as a selectable table: name and summary, status, model, enable switch and row menu. */
export function AgentTable({
  agents, selectedIds, onSelectionChange, bulkActions, empty, readOnly = false,
  onOpen, onChat, onToggle, onResummon, onDelete,
}: AgentTableProps) {
  const { t } = useTranslation('agents')

  const nameOf = (a: AgentData) => agentDisplayName(a) ?? t('screen.list.unnamed')

  const columns: DataTableColumn<AgentData>[] = [
    {
      id: 'agent',
      header: t('screen.list.colAgent'),
      className: 'min-w-[240px]',
      cell: (a) => (
        <div className="flex min-w-0 items-center gap-3">
          <AgentBadge emoji={agentEmoji(a)} size="sm" />
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="truncate text-ui font-semibold text-ink">{nameOf(a)}</span>
              {a.is_default && <span className="shrink-0 text-caption text-accent-subtle-ink">{t('screen.list.defaultBadge')}</span>}
            </div>
            <p className="max-w-md truncate text-caption font-normal text-ink-muted">{agentSummary(a) || a.agent_key}</p>
          </div>
        </div>
      ),
    },
    {
      id: 'status',
      header: t('screen.list.colStatus'),
      className: 'w-36',
      cell: (a) => {
        const v = agentStatusView(a.status)
        return <StatusChip tone={v.tone} label={t(`screen.status.${v.key}`)} />
      },
    },
    {
      id: 'model',
      header: t('screen.list.colModel'),
      className: 'w-48',
      cell: (a) => (
        <span className="block truncate text-code text-ink-secondary" title={[a.provider, a.model].filter(Boolean).join(' / ')}>
          {a.model || '—'}
        </span>
      ),
    },
    {
      id: 'enabled',
      header: t('screen.list.colEnabled'),
      className: 'w-24',
      cell: (a) => (
        <Switch
          checked={isAgentEnabled(a)}
          disabled={readOnly || a.status === 'summoning' || a.status === 'summon_failed'}
          aria-label={t('screen.list.enableAgent', { name: nameOf(a) })}
          onCheckedChange={(v) => onToggle(a, v)}
        />
      ),
    },
    {
      id: 'actions',
      header: <span className="sr-only">{t('screen.list.colActions')}</span>,
      className: 'w-14 text-right',
      cell: (a) => {
        const items: MenuEntry[] = [
          { label: t('screen.menu.open'), onSelect: () => onOpen(a) },
          { label: t('screen.menu.chat'), icon: <IconChat size={14} />, onSelect: () => onChat(a), disabled: a.status !== 'active' },
        ]
        if (a.status === 'summon_failed') {
          items.push({ label: t('screen.menu.resummon'), icon: <IconRefresh size={14} />, onSelect: () => onResummon(a), disabled: readOnly })
        }
        items.push('separator', {
          label: t('screen.menu.delete'),
          icon: <IconTrash size={14} />,
          destructive: true,
          disabled: readOnly,
          onSelect: () => onDelete(a),
        })
        return (
          <div className="flex justify-end" onClick={(e) => e.stopPropagation()}>
            <Menu items={items} label={t('screen.list.rowActions', { name: nameOf(a) })} />
          </div>
        )
      },
    },
  ]

  return (
    <DataTable
      aria-label={t('screen.list.tableLabel')}
      columns={columns}
      rows={agents}
      getRowId={(a) => a.id}
      selectable
      selectedIds={selectedIds}
      onSelectionChange={onSelectionChange}
      bulkActions={bulkActions}
      onRowClick={onOpen}
      empty={empty}
    />
  )
}

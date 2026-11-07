import { useTranslation } from 'react-i18next'
import { DataTable, Menu, StatusChip, Switch, type DataTableColumn, type MenuEntry } from '../ui'
import { IconTrash } from '../common/Icons'
import { ChannelTypeIcon } from './channel-type-icon'
import { getChannelStatusChip } from '../../utils/channel-status'
import type { ChannelInstanceData, ChannelStatus } from '../../types/channel'

interface ChannelTableProps {
  instances: ChannelInstanceData[]
  /** Runtime status keyed by instance name. */
  statusMap: Record<string, ChannelStatus>
  /** Agent display names keyed by agent id. */
  agentNames: Map<string, string>
  /** Disables writes while the gateway is unreachable. */
  readOnly?: boolean
  onToggle: (instance: ChannelInstanceData, enabled: boolean) => void
  onOpen: (instance: ChannelInstanceData) => void
  onDelete: (instance: ChannelInstanceData) => void
}

/** Channel instances as a table: platform and name, agent, status, enable switch and row menu. */
export function ChannelTable({ instances, statusMap, agentNames, readOnly = false, onToggle, onOpen, onDelete }: ChannelTableProps) {
  const { t } = useTranslation('channels')
  const nameOf = (i: ChannelInstanceData) => i.display_name || i.name

  const columns: DataTableColumn<ChannelInstanceData>[] = [
    {
      id: 'channel',
      header: t('screen.columns.channel'),
      className: 'min-w-[240px]',
      cell: (i) => (
        <div className="flex min-w-0 items-center gap-3">
          <span className="shrink-0 text-ink-secondary"><ChannelTypeIcon type={i.channel_type} /></span>
          <div className="min-w-0">
            <p className="truncate text-ui font-semibold text-ink">{nameOf(i)}</p>
            <p className="truncate text-caption text-ink-muted">{t(`channelTypes.${i.channel_type}`, { defaultValue: i.channel_type })}</p>
          </div>
        </div>
      ),
    },
    {
      id: 'agent',
      header: t('screen.columns.agent'),
      className: 'min-w-[140px]',
      cell: (i) => <span className="truncate text-ui text-ink-secondary">{agentNames.get(i.agent_id) ?? i.agent_id.slice(0, 8)}</span>,
    },
    {
      id: 'status',
      header: t('screen.columns.status'),
      className: 'min-w-[180px]',
      cell: (i) => {
        const status = statusMap[i.name] ?? null
        const chip = getChannelStatusChip(i.enabled, status, t)
        const hint = i.enabled && status?.state !== 'healthy' ? status?.summary : undefined
        return (
          <div className="flex min-w-0 flex-col items-start gap-1">
            <StatusChip tone={chip.tone} label={chip.label} title={status?.detail || status?.summary || undefined} />
            {hint && <p className="max-w-xs truncate text-caption text-ink-muted" title={hint}>{hint}</p>}
          </div>
        )
      },
    },
    {
      id: 'enabled',
      header: t('screen.columns.enabled'),
      className: 'w-24',
      cell: (i) => (
        <div onClick={(e) => e.stopPropagation()}>
          <Switch
            checked={i.enabled}
            disabled={readOnly}
            aria-label={t('screen.enableChannel', { name: nameOf(i) })}
            onCheckedChange={(v) => onToggle(i, v)}
          />
        </div>
      ),
    },
    {
      id: 'actions',
      header: <span className="sr-only">{t('screen.columns.actions')}</span>,
      className: 'w-14 text-right',
      cell: (i) => {
        const items: MenuEntry[] = [
          { label: t('screen.menu.open'), onSelect: () => onOpen(i) },
          'separator',
          { label: t('screen.menu.delete'), icon: <IconTrash size={14} />, destructive: true, disabled: readOnly, onSelect: () => onDelete(i) },
        ]
        return (
          <div className="flex justify-end" onClick={(e) => e.stopPropagation()}>
            <Menu items={items} label={t('screen.rowActions', { name: nameOf(i) })} />
          </div>
        )
      },
    },
  ]

  return (
    <DataTable
      aria-label={t('screen.tableLabel')}
      columns={columns}
      rows={instances}
      getRowId={(i) => i.id}
      onRowClick={onOpen}
    />
  )
}

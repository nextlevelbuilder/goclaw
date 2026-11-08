import { useTranslation } from 'react-i18next'
import { DataTable, Menu, StatusChip, Switch, type DataTableColumn, type MenuEntry } from '../ui'
import { IconRefresh, IconTrash } from '../common/Icons'
import type { MCPServerData } from '../../types/mcp'

interface McpServerTableProps {
  servers: MCPServerData[]
  /** Disables writes while the gateway is unreachable. */
  readOnly?: boolean
  reconnectingId: string | null
  onToggle: (server: MCPServerData, enabled: boolean) => void
  onEdit: (server: MCPServerData) => void
  onDelete: (server: MCPServerData) => void
  onViewTools: (server: MCPServerData) => void
  onManageGrants: (server: MCPServerData) => void
  onReconnect: (server: MCPServerData) => void
}

const TRANSPORT_KEY: Record<MCPServerData['transport'], string> = {
  stdio: 'stdio',
  sse: 'sse',
  'streamable-http': 'http',
}

/** The endpoint a server talks to: the command line for local processes, the URL for remote ones. */
function endpointOf(s: MCPServerData): string {
  if (s.transport === 'stdio') return [s.command, ...(s.args ?? [])].filter(Boolean).join(' ')
  return s.url
}

/** MCP servers as a table: name and endpoint, transport, state, agents, enable switch and row menu. */
export function McpServerTable({
  servers, readOnly = false, reconnectingId,
  onToggle, onEdit, onDelete, onViewTools, onManageGrants, onReconnect,
}: McpServerTableProps) {
  const { t } = useTranslation('mcp')
  const nameOf = (s: MCPServerData) => s.display_name || s.name

  const columns: DataTableColumn<MCPServerData>[] = [
    {
      id: 'server',
      header: t('columns.name'),
      className: 'min-w-[240px]',
      cell: (s) => (
        <div className="min-w-0">
          <p className="truncate text-ui font-semibold text-ink">{nameOf(s)}</p>
          <p className="max-w-md truncate font-mono text-code text-ink-muted" title={endpointOf(s)}>{endpointOf(s) || `mcp_${s.name}`}</p>
        </div>
      ),
    },
    {
      id: 'transport',
      header: t('columns.transport'),
      className: 'w-40',
      cell: (s) => <span className="text-ui text-ink-secondary">{t(`screen.transport.${TRANSPORT_KEY[s.transport]}`)}</span>,
    },
    {
      id: 'status',
      header: t('screen.list.colStatus'),
      className: 'w-32',
      cell: (s) => (
        <StatusChip tone={s.enabled ? 'success' : 'idle'} label={s.enabled ? t('screen.list.statusOn') : t('screen.list.statusOff')} />
      ),
    },
    {
      id: 'agents',
      header: t('columns.agents'),
      className: 'w-24',
      cell: (s) => (
        <button
          type="button"
          onClick={(e) => { e.stopPropagation(); onManageGrants(s) }}
          title={t('manageGrants')}
          className="rounded-sm font-mono text-code text-ink-secondary hover:text-ink"
        >
          {s.agent_count ?? 0}
        </button>
      ),
    },
    {
      id: 'enabled',
      header: t('columns.enabled'),
      className: 'w-24',
      cell: (s) => (
        <Switch
          checked={s.enabled}
          disabled={readOnly}
          aria-label={t('screen.list.enableServer', { name: nameOf(s) })}
          onCheckedChange={(v) => onToggle(s, v)}
        />
      ),
    },
    {
      id: 'actions',
      header: <span className="sr-only">{t('columns.actions')}</span>,
      className: 'w-14 text-right',
      cell: (s) => {
        const items: MenuEntry[] = [
          { label: t('screen.menu.edit'), onSelect: () => onEdit(s), disabled: readOnly },
          { label: t('viewTools'), onSelect: () => onViewTools(s) },
          { label: t('manageGrants'), onSelect: () => onManageGrants(s) },
          { label: t('reconnect'), icon: <IconRefresh size={14} />, onSelect: () => onReconnect(s), disabled: readOnly || reconnectingId === s.id },
          'separator',
          { label: t('screen.menu.delete'), icon: <IconTrash size={14} />, destructive: true, disabled: readOnly, onSelect: () => onDelete(s) },
        ]
        return (
          <div className="flex justify-end" onClick={(e) => e.stopPropagation()}>
            <Menu items={items} label={t('screen.list.rowActions', { name: nameOf(s) })} />
          </div>
        )
      },
    },
  ]

  return (
    <DataTable
      aria-label={t('screen.list.tableLabel')}
      columns={columns}
      rows={servers}
      getRowId={(s) => s.id}
      onRowClick={onEdit}
    />
  )
}

import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMcpServers, MAX_MCP_LITE } from '../../hooks/use-mcp-servers'
import { useAgentCrud } from '../../hooks/use-agent-crud'
import { useGatewayStatus } from '../layout/use-gateway-status'
import { cn } from '../../lib/cn'
import { Button, ConfirmDialog, EmptyState, ErrorState, IconButton, Input, PageHeader, SkeletonList } from '../ui'
import { IconPlus, IconRefresh } from '../common/Icons'
import { McpFormDialog } from './McpFormDialog'
import { McpGrantsDialog } from './McpGrantsDialog'
import { McpToolsDialog } from './McpToolsDialog'
import { McpServerTable } from './mcp-server-table'
import type { MCPServerData } from '../../types/mcp'

export function McpServerList() {
  const { t } = useTranslation(['mcp', 'common'])
  const {
    servers, loading, error, atLimit,
    fetchServers, createServer, updateServer, setServerEnabled, deleteServer,
    testConnection, reconnectServer, listServerTools,
    listGrants, grantAgent, revokeAgent,
  } = useMcpServers()
  const { agents } = useAgentCrud()
  const { disconnected } = useGatewayStatus()

  const [query, setQuery] = useState('')
  const [formOpen, setFormOpen] = useState(false)
  const [editServer, setEditServer] = useState<MCPServerData | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<MCPServerData | null>(null)
  const [grantsServer, setGrantsServer] = useState<MCPServerData | null>(null)
  const [toolsServer, setToolsServer] = useState<MCPServerData | null>(null)
  const [reconnectingId, setReconnectingId] = useState<string | null>(null)
  const [refreshing, setRefreshing] = useState(false)

  const openCreate = () => { setEditServer(null); setFormOpen(true) }
  const openEdit = (s: MCPServerData) => { if (!disconnected) { setEditServer(s); setFormOpen(true) } }

  const handleReconnect = async (s: MCPServerData) => {
    setReconnectingId(s.id)
    try { await reconnectServer(s.id) } catch { /* reported by the hook */ } finally { setReconnectingId(null) }
  }

  const handleRefresh = async () => {
    setRefreshing(true)
    try { await fetchServers() } finally { setRefreshing(false) }
  }

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return servers
    return servers.filter((s) => `${s.name} ${s.display_name} ${s.url} ${s.command}`.toLowerCase().includes(q))
  }, [servers, query])

  const enabledCount = servers.filter((s) => s.enabled).length
  const name = (s: MCPServerData) => s.display_name || s.name

  const addButton = (
    <Button variant="primary" icon={<IconPlus size={14} />} onClick={openCreate} disabled={atLimit || disconnected}>
      {t('addServer')}
    </Button>
  )

  let body
  if (loading) {
    body = <SkeletonList rows={4} label={t('screen.list.loading')} />
  } else if (error && servers.length === 0) {
    body = (
      <ErrorState
        title={t('screen.list.loadError')}
        description={t('screen.list.loadErrorHint')}
        onRetry={() => { void handleRefresh() }}
        retrying={refreshing}
      />
    )
  } else if (servers.length === 0) {
    body = (
      <EmptyState
        title={t('emptyTitle')}
        description={t('emptyDescription')}
        action={<Button variant="primary" icon={<IconPlus size={14} />} onClick={openCreate} disabled={disconnected}>{t('addServer')}</Button>}
      />
    )
  } else if (filtered.length === 0) {
    body = (
      <EmptyState
        title={t('noMatchTitle')}
        description={t('noMatchDescription')}
        action={<Button onClick={() => setQuery('')}>{t('screen.list.clearSearch')}</Button>}
      />
    )
  } else {
    body = (
      <McpServerTable
        servers={filtered}
        readOnly={disconnected}
        reconnectingId={reconnectingId}
        onToggle={(s, v) => { void setServerEnabled(s.id, v) }}
        onEdit={openEdit}
        onDelete={setDeleteTarget}
        onViewTools={setToolsServer}
        onManageGrants={setGrantsServer}
        onReconnect={(s) => { void handleReconnect(s) }}
      />
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader
        title={t('title')}
        description={t('screen.list.description', { count: servers.length, enabled: enabledCount })}
        actions={
          <>
            <IconButton
              aria-label={t('common:refresh')}
              icon={<IconRefresh size={16} />}
              onClick={() => { void handleRefresh() }}
              disabled={refreshing}
            />
            {addButton}
          </>
        }
      />

      <div className={cn('flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-6 py-4', disconnected && 'opacity-60')}>
        {servers.length > 0 && (
          <Input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t('searchPlaceholder')}
            aria-label={t('searchPlaceholder')}
            className="max-w-sm"
          />
        )}
        {atLimit && (
          <p role="status" className="rounded-md bg-warning-subtle px-3 py-2 text-caption text-warning">
            {t('screen.list.limitReached', { max: MAX_MCP_LITE })}
          </p>
        )}
        {body}
      </div>

      <McpFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        server={editServer}
        onSubmit={async (data) => {
          if (editServer) await updateServer(editServer.id, data)
          else await createServer(data)
        }}
        onTest={testConnection}
        readOnly={disconnected}
        onDelete={editServer ? () => { setFormOpen(false); setDeleteTarget(editServer) } : undefined}
      />

      <ConfirmDialog
        open={deleteTarget !== null}
        onOpenChange={(open) => { if (!open) setDeleteTarget(null) }}
        title={deleteTarget ? t('screen.delete.title', { name: name(deleteTarget) }) : ''}
        description={t('screen.delete.description')}
        confirmLabel={t('delete.confirmLabel')}
        variant="destructive"
        confirmValue={deleteTarget?.name}
        onConfirm={async () => {
          if (!deleteTarget) return
          try { await deleteServer(deleteTarget.id); setDeleteTarget(null) } catch { /* reported by the hook */ }
        }}
      />

      {grantsServer && (
        <McpGrantsDialog
          open
          onOpenChange={() => setGrantsServer(null)}
          server={grantsServer}
          agents={agents}
          readOnly={disconnected}
          onLoadGrants={listGrants}
          onGrant={grantAgent}
          onRevoke={revokeAgent}
        />
      )}

      {toolsServer && (
        <McpToolsDialog
          open
          onOpenChange={() => setToolsServer(null)}
          server={toolsServer}
          onLoadTools={listServerTools}
        />
      )}
    </div>
  )
}

import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useMcpServers, MAX_MCP_LITE } from '../../hooks/use-mcp-servers'
import { useAgentCrud } from '../../hooks/use-agent-crud'
import { McpFormDialog } from './McpFormDialog'
import { McpGrantsDialog } from './McpGrantsDialog'
import { McpToolsDialog } from './McpToolsDialog'
import { ConfirmDeleteDialog } from '../common/ConfirmDeleteDialog'
import { RefreshButton } from '../common/RefreshButton'
import { McpServerRow } from './mcp-server-row'
import type { MCPServerData } from '../../types/mcp'

export function McpServerList() {
  const { t } = useTranslation(['mcp', 'common'])
  const {
    servers, loading, atLimit,
    fetchServers, createServer, updateServer, deleteServer,
    testConnection, reconnectServer, listServerTools,
    listGrants, grantAgent, revokeAgent,
  } = useMcpServers()
  const { agents } = useAgentCrud()

  const [formOpen, setFormOpen] = useState(false)
  const [editServer, setEditServer] = useState<MCPServerData | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<MCPServerData | null>(null)
  const [grantsServer, setGrantsServer] = useState<MCPServerData | null>(null)
  const [toolsServer, setToolsServer] = useState<MCPServerData | null>(null)
  const [reconnectingId, setReconnectingId] = useState<string | null>(null)

  function openCreate() { setEditServer(null); setFormOpen(true) }
  function openEdit(s: MCPServerData) { setEditServer(s); setFormOpen(true) }

  async function handleReconnect(id: string) {
    setReconnectingId(id)
    try { await reconnectServer(id) } finally { setReconnectingId(null) }
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">{t('title')}</h2>
          <p className="text-xs text-text-muted mt-0.5">{t('description')} (max {MAX_MCP_LITE})</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={openCreate}
            disabled={atLimit}
            className="bg-accent text-white rounded-lg px-3 py-1.5 text-xs hover:bg-accent-hover disabled:opacity-50 transition-colors flex items-center gap-1.5"
          >
            <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M5 12h14" /><path d="M12 5v14" />
            </svg>
            {t('addServer')}
          </button>
          <RefreshButton onRefresh={fetchServers} />
        </div>
      </div>

      {atLimit && (
        <p className="text-[11px] text-amber-600 dark:text-amber-400">
          {t('noMatchTitle')} ({MAX_MCP_LITE}). {t('noMatchDescription')}
        </p>
      )}

      {/* Loading skeleton */}
      {loading ? (
        <div className="space-y-2">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-12 rounded-lg bg-surface-tertiary/50 animate-pulse" />
          ))}
        </div>
      ) : servers.length === 0 ? (
        <div className="flex flex-col items-center gap-2 py-12">
          <svg className="h-10 w-10 text-text-muted/40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
            <path d="M12 22v-5" /><path d="M9 8V2" /><path d="M15 8V2" />
            <path d="M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8Z" />
          </svg>
          <p className="text-sm text-text-muted">{t('emptyTitle')}</p>
          <p className="text-xs text-text-muted/70">{t('emptyDescription')}</p>
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="w-full text-sm">
            <thead>
              <tr className="border-b border-border bg-surface-tertiary/40">
                <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.name')}</th>
                <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.transport')}</th>
                <th className="px-4 py-2.5 text-center text-xs font-medium text-text-muted">{t('columns.tools')}</th>
                <th className="px-4 py-2.5 text-center text-xs font-medium text-text-muted">{t('columns.agents')}</th>
                <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.enabled')}</th>
                <th className="px-4 py-2.5 text-right text-xs font-medium text-text-muted">{t('columns.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {servers.map((s) => (
                <McpServerRow
                  key={s.id}
                  server={s}
                  reconnectingId={reconnectingId}
                  onReconnect={handleReconnect}
                  onEdit={openEdit}
                  onDelete={(srv) => setDeleteTarget(srv)}
                  onViewTools={(srv) => setToolsServer(srv)}
                  onManageGrants={(srv) => setGrantsServer(srv)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      <McpFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        server={editServer}
        onSubmit={async (data) => {
          if (editServer) await updateServer(editServer.id, data)
          else await createServer(data)
        }}
        onTest={testConnection}
      />

      {deleteTarget && (
        <ConfirmDeleteDialog
          open
          onOpenChange={() => setDeleteTarget(null)}
          title="Delete MCP Server"
          description={`This will permanently delete the server "${deleteTarget.display_name || deleteTarget.name}" and all its agent grants.`}
          confirmValue={deleteTarget.name}
          onConfirm={async () => { await deleteServer(deleteTarget.id); setDeleteTarget(null) }}
        />
      )}

      {grantsServer && (
        <McpGrantsDialog
          open
          onOpenChange={() => setGrantsServer(null)}
          server={grantsServer}
          agents={agents}
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

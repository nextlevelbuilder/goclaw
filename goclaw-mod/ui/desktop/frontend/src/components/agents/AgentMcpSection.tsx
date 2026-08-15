import { useState, useEffect, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { getApiClient, isApiClientReady } from '../../lib/api'
import { Switch } from '../common/Switch'
import type { MCPServerData, MCPAgentGrant } from '../../types/mcp'

interface AgentMcpSectionProps {
  agentId: string
}

export function AgentMcpSection({ agentId }: AgentMcpSectionProps) {
  const { t } = useTranslation('mcp')
  const [servers, setServers] = useState<MCPServerData[]>([])
  const [grants, setGrants] = useState<MCPAgentGrant[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState('')

  const fetchData = useCallback(async () => {
    if (!isApiClientReady()) return
    try {
      const [srvRes, grantRes] = await Promise.all([
        getApiClient().get<{ servers: MCPServerData[] | null }>('/v1/mcp/servers'),
        getApiClient().get<{ grants: MCPAgentGrant[] | null }>(`/v1/mcp/grants/agent/${agentId}`),
      ])
      setServers((srvRes.servers ?? []).filter((s) => s.enabled))
      setGrants(grantRes.grants ?? [])
    } catch (err) {
      console.error('Failed to fetch MCP data:', err)
    } finally {
      setLoading(false)
    }
  }, [agentId])

  useEffect(() => { fetchData() }, [fetchData])

  const grantedIds = new Set(grants.map((g) => g.server_id))

  async function handleToggle(server: MCPServerData) {
    const isGranted = grantedIds.has(server.id)
    setError('')

    // Optimistic update
    if (isGranted) {
      setGrants((prev) => prev.filter((g) => g.server_id !== server.id))
    } else {
      setGrants((prev) => [...prev, { id: '', server_id: server.id, agent_id: agentId, enabled: true, tool_allow: null, tool_deny: null, granted_by: '', created_at: '' }])
    }

    try {
      if (isGranted) {
        await getApiClient().delete(`/v1/mcp/servers/${server.id}/grants/agent/${agentId}`)
      } else {
        await getApiClient().post(`/v1/mcp/servers/${server.id}/grants/agent`, { agent_id: agentId })
      }
    } catch (err) {
      setError((err as Error).message || 'Failed to update MCP grant')
      // Revert
      if (isGranted) {
        setGrants((prev) => [...prev, { id: '', server_id: server.id, agent_id: agentId, enabled: true, tool_allow: null, tool_deny: null, granted_by: '', created_at: '' }])
      } else {
        setGrants((prev) => prev.filter((g) => g.server_id !== server.id))
      }
    }
  }

  const grantedCount = servers.filter((s) => grantedIds.has(s.id)).length

  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-semibold text-text-primary">{t('title')}</h3>
        {!loading && servers.length > 0 && (
          <span className="text-[11px] text-text-muted">
            {t('grants.currentGrants')}: {grantedCount}/{servers.length}
          </span>
        )}
      </div>

      {error && <p className="text-xs text-error">{error}</p>}

      {loading ? (
        <div className="space-y-2">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-9 rounded-lg bg-surface-tertiary/50 animate-pulse" />
          ))}
        </div>
      ) : servers.length === 0 ? (
        <p className="text-xs text-text-muted py-3 text-center">{t('emptyTitle')}</p>
      ) : (
        <div className="rounded-lg border border-border divide-y divide-border">
          {servers.map((server) => (
            <div key={server.id} className="flex items-center justify-between px-3 py-2 hover:bg-surface-tertiary/30 transition-colors">
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <svg className="h-3.5 w-3.5 text-text-muted shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                    <path d="M12 22v-5" /><path d="M9 8V2" /><path d="M15 8V2" />
                    <path d="M18 8v5a4 4 0 0 1-4 4h-4a4 4 0 0 1-4-4V8Z" />
                  </svg>
                  <span className="text-xs font-medium text-text-primary truncate">
                    {server.display_name || server.name}
                  </span>
                  <span className={`rounded-full px-1.5 py-0.5 text-[9px] font-medium ${
                    server.transport === 'sse'
                      ? 'bg-surface-tertiary text-text-secondary'
                      : 'border border-border text-text-muted'
                  }`}>
                    {server.transport.toUpperCase()}
                  </span>
                </div>
                <p className="text-[11px] text-text-muted font-mono ml-5">mcp_{server.name}</p>
              </div>
              <Switch
                checked={grantedIds.has(server.id)}
                onCheckedChange={() => handleToggle(server)}
              />
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

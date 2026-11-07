import { useEffect, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Button, Dialog, ErrorState, Field, SkeletonList } from '../ui'
import { Combobox } from '../common/Combobox'
import type { MCPServerData, MCPAgentGrant } from '../../types/mcp'
import type { AgentData } from '../../types/agent'

interface McpGrantsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  server: MCPServerData
  agents: AgentData[]
  /** Disables grant and revoke while the gateway is unreachable. */
  readOnly?: boolean
  onLoadGrants: (serverId: string) => Promise<MCPAgentGrant[]>
  onGrant: (serverId: string, agentId: string) => Promise<void>
  onRevoke: (serverId: string, agentId: string) => Promise<void>
}

export function McpGrantsDialog({ open, onOpenChange, server, agents, readOnly = false, onLoadGrants, onGrant, onRevoke }: McpGrantsDialogProps) {
  const { t } = useTranslation(['mcp', 'common'])
  const [grants, setGrants] = useState<MCPAgentGrant[]>([])
  const [loading, setLoading] = useState(false)
  const [loadFailed, setLoadFailed] = useState(false)
  const [selectedAgent, setSelectedAgent] = useState('')
  const [granting, setGranting] = useState(false)
  const [revokingId, setRevokingId] = useState<string | null>(null)
  const [error, setError] = useState('')

  const refresh = useCallback(async () => {
    setLoading(true)
    setLoadFailed(false)
    try {
      setGrants(await onLoadGrants(server.id))
    } catch (err) {
      console.error('Failed to load MCP grants:', err)
      setLoadFailed(true)
    } finally {
      setLoading(false)
    }
  }, [server.id, onLoadGrants])

  useEffect(() => {
    if (open) void refresh()
  }, [open, refresh])

  const grantedIds = new Set(grants.map((g) => g.agent_id))
  const availableAgents = agents
    .filter((a) => !grantedIds.has(a.id))
    .map((a) => ({ value: a.id, label: a.display_name || a.agent_key }))

  const agentName = (agentId: string) => {
    const a = agents.find((x) => x.id === agentId)
    return a?.display_name || a?.agent_key || agentId.slice(0, 8)
  }

  async function handleGrant() {
    if (!selectedAgent) return
    setGranting(true)
    setError('')
    try {
      await onGrant(server.id, selectedAgent)
      setSelectedAgent('')
      setGrants(await onLoadGrants(server.id))
    } catch (err) {
      console.error('Failed to grant MCP access:', err)
      setError((err as Error).message || t('grants.failedGrant'))
    } finally {
      setGranting(false)
    }
  }

  async function handleRevoke(agentId: string) {
    setError('')
    setRevokingId(agentId)
    try {
      await onRevoke(server.id, agentId)
      await refresh()
    } catch (err) {
      setError((err as Error).message || t('grants.failedRevoke'))
    } finally {
      setRevokingId(null)
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      title={t('grants.title', { name: server.display_name || server.name })}
      description={t('screen.grants.description')}
    >
      <div className="flex flex-col gap-4">
        {loading ? (
          <SkeletonList rows={2} avatar={false} label={t('common:loading')} />
        ) : loadFailed ? (
          <ErrorState title={t('screen.grants.loadError')} onRetry={() => { void refresh() }} className="py-6" />
        ) : grants.length === 0 ? (
          <p className="py-4 text-center text-ui font-normal text-ink-muted">{t('grants.noAgentsGranted')}</p>
        ) : (
          <ul className="flex flex-col gap-2">
            {grants.map((grant) => (
              <li key={grant.id} className="flex items-center justify-between gap-3 rounded-lg border border-border-subtle px-3 py-2">
                <span className="min-w-0 truncate text-ui text-ink">{agentName(grant.agent_id)}</span>
                <Button size="sm" onClick={() => { void handleRevoke(grant.agent_id) }} loading={revokingId === grant.agent_id} disabled={readOnly}>
                  {t('grants.revoke')}
                </Button>
              </li>
            ))}
          </ul>
        )}

        {error && <p role="alert" className="text-caption text-danger">{error}</p>}

        {availableAgents.length > 0 && (
          <div className="flex items-end gap-2 border-t border-border-subtle pt-4">
            <Field label={t('grants.addGrant')} className="min-w-0 flex-1">
              <Combobox
                value={selectedAgent}
                onChange={setSelectedAgent}
                options={availableAgents}
                placeholder={t('grants.selectAgent')}
                disabled={readOnly}
              />
            </Field>
            <Button variant="primary" onClick={() => { void handleGrant() }} disabled={!selectedAgent || readOnly} loading={granting}>
              {t('grants.grant')}
            </Button>
          </div>
        )}
      </div>
    </Dialog>
  )
}

import { useEffect, useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { Combobox } from '../common/Combobox'
import type { MCPServerData, MCPAgentGrant } from '../../types/mcp'
import type { AgentData } from '../../types/agent'

interface McpGrantsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  server: MCPServerData
  agents: AgentData[]
  onLoadGrants: (serverId: string) => Promise<MCPAgentGrant[]>
  onGrant: (serverId: string, agentId: string) => Promise<void>
  onRevoke: (serverId: string, agentId: string) => Promise<void>
}

export function McpGrantsDialog({ open, onOpenChange, server, agents, onLoadGrants, onGrant, onRevoke }: McpGrantsDialogProps) {
  const { t } = useTranslation(['mcp', 'common'])
  const [grants, setGrants] = useState<MCPAgentGrant[]>([])
  const [loading, setLoading] = useState(false)
  const [selectedAgent, setSelectedAgent] = useState('')
  const [granting, setGranting] = useState(false)
  const [error, setError] = useState('')

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      const g = await onLoadGrants(server.id)
      setGrants(g)
    } catch (err) {
      console.error('Failed to load MCP grants:', err)
    } finally {
      setLoading(false)
    }
  }, [server.id, onLoadGrants])

  useEffect(() => {
    if (open) refresh()
  }, [open, refresh])

  if (!open) return null

  const grantedIds = new Set(grants.map((g) => g.agent_id))
  const availableAgents = agents
    .filter((a) => !grantedIds.has(a.id))
    .map((a) => ({ value: a.id, label: a.display_name || a.agent_key }))

  function agentName(agentId: string): string {
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
      const updated = await onLoadGrants(server.id)
      setGrants(updated)
    } catch (err) {
      console.error('Failed to grant MCP access:', err)
      setError((err as Error).message || 'Failed to grant')
    } finally {
      setGranting(false)
    }
  }

  async function handleRevoke(agentId: string) {
    setError('')
    try {
      await onRevoke(server.id, agentId)
      await refresh()
    } catch (err) {
      setError((err as Error).message || 'Failed to revoke')
    }
  }

  const displayName = server.display_name || server.name

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" onClick={() => onOpenChange(false)} />
      <div className="relative w-full max-w-md bg-surface-secondary rounded-xl border border-border overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-5 py-4">
          <div>
            <div className="flex items-center gap-2">
              <svg className="h-4 w-4 text-text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
                <circle cx="9" cy="7" r="4" />
                <path d="M22 21v-2a4 4 0 0 0-3-3.87" /><path d="M16 3.13a4 4 0 0 1 0 7.75" />
              </svg>
              <span className="text-sm font-semibold text-text-primary">{t('grants.title', { name: displayName })}</span>
            </div>
            <p className="text-xs text-text-muted mt-0.5">{t('grants.addGrant')}</p>
          </div>
          <button onClick={() => onOpenChange(false)} className="p-1 text-text-muted hover:text-text-primary transition-colors">
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M18 6 6 18" /><path d="m6 6 12 12" />
            </svg>
          </button>
        </div>

        {/* Content */}
        <div className="p-5 space-y-4">
          {/* Current grants */}
          <div className="space-y-2">
            {loading ? (
              <div className="flex items-center justify-center gap-2 py-4">
                <svg className="h-4 w-4 animate-spin text-text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}><path d="M21 12a9 9 0 1 1-6.219-8.56" /></svg>
              </div>
            ) : grants.length === 0 ? (
              <p className="text-xs text-text-muted text-center py-4">{t('grants.noAgentsGranted')}</p>
            ) : (
              grants.map((grant) => (
                <div key={grant.id} className="flex items-center justify-between border border-border rounded-lg px-3 py-2">
                  <span className="text-sm text-text-primary">{agentName(grant.agent_id)}</span>
                  <button
                    onClick={() => handleRevoke(grant.agent_id)}
                    className="text-[11px] text-text-muted hover:text-error transition-colors flex items-center gap-1"
                  >
                    <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                      <path d="M18 6 6 18" /><path d="m6 6 12 12" />
                    </svg>
                    {t('grants.revoke')}
                  </button>
                </div>
              ))
            )}
          </div>

          {error && <p className="text-xs text-error">{error}</p>}

          {/* Add grant */}
          {availableAgents.length > 0 && (
            <div className="flex items-end gap-2 border-t border-border pt-4">
              <div className="flex-1">
                <label className="text-xs font-medium text-text-secondary mb-1 block">{t('grants.addGrant')}</label>
                <Combobox
                  value={selectedAgent}
                  onChange={setSelectedAgent}
                  options={availableAgents}
                  placeholder={t('grants.selectAgent')}
                />
              </div>
              <button
                onClick={handleGrant}
                disabled={!selectedAgent || granting}
                className="bg-accent text-white rounded-lg px-4 py-2.5 text-xs hover:bg-accent-hover disabled:opacity-50 transition-colors shrink-0"
              >
                {granting ? t('common:loading') : t('grants.grant')}
              </button>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

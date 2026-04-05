import { useState, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { useAgentCrud } from '../../../hooks/use-agent-crud'
import { useAgentStore } from '../../../stores/agent-store'
import { AgentCard } from './AgentCard'
import { AgentFormDialog } from './AgentFormDialog'
import { AgentDetailPanel } from './AgentDetailPanel'
import { ConfirmDeleteDialog } from '../../common/ConfirmDeleteDialog'
import { SummoningModal } from '../../onboarding/SummoningModal'
import type { AgentData, AgentInput } from '../../../types/agent'

export function AgentList() {
  const { t } = useTranslation(['agents', 'common'])
  const { agents, loading, atLimit, createAgent, updateAgent, deleteAgent, resummonAgent, fetchAgents } = useAgentCrud()
  const setStoreAgents = useAgentStore((s) => s.setAgents)

  const [formOpen, setFormOpen] = useState(false)
  const [detailAgent, setDetailAgent] = useState<AgentData | null>(null)
  const [deletingAgent, setDeletingAgent] = useState<AgentData | null>(null)
  const [summoningAgent, setSummoningAgent] = useState<{ id: string; name: string } | null>(null)

  const refreshSidebar = useCallback(() => {
    setStoreAgents(agents.map((a) => {
      const otherCfg = typeof a.other_config === 'object' && a.other_config !== null ? a.other_config as Record<string, unknown> : {}
      return {
        id: a.id,
        key: a.agent_key,
        name: a.display_name || a.agent_key,
        model: a.model ?? 'unknown',
        status: 'online' as const,
        emoji: typeof otherCfg.emoji === 'string' ? otherCfg.emoji : undefined,
      }
    }))
  }, [agents, setStoreAgents])

  const handleCreate = async (input: AgentInput) => {
    const created = await createAgent(input)
    if (created.status === 'summoning') {
      setSummoningAgent({ id: created.id, name: created.display_name || created.agent_key })
    }
    refreshSidebar()
    return created
  }

  const handleSave = async (id: string, updates: Partial<AgentData>) => {
    await updateAgent(id, updates)
    await fetchAgents()
    refreshSidebar()
  }

  const handleDelete = async () => {
    if (!deletingAgent) return
    await deleteAgent(deletingAgent.id)
    setDeletingAgent(null)
    setDetailAgent(null)
    refreshSidebar()
  }

  const handleResummon = async (agent: AgentData) => {
    await resummonAgent(agent.id)
    setSummoningAgent({ id: agent.id, name: agent.display_name || agent.agent_key })
  }

  const handleSummoningComplete = () => {
    setSummoningAgent(null)
    fetchAgents().then(refreshSidebar)
  }

  if (loading) {
    return <p className="text-xs text-text-muted py-4">{t('common:loading', 'Loading...')}</p>
  }

  return (
    <>
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-semibold text-text-primary">{t('agents:title')}</h3>
          <button
            onClick={() => setFormOpen(true)}
            disabled={atLimit}
            className="px-3 py-1.5 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-50"
          >
            + {t('agents:createAgent')}
          </button>
        </div>

        {atLimit && (
          <p className="text-xs text-warning">Max 5 agents in Lite edition.</p>
        )}

        {agents.length === 0 ? (
          <p className="text-xs text-text-muted py-4 text-center">{t('agents:emptyTitle')}</p>
        ) : (
          <div className="grid grid-cols-1 gap-2">
            {agents.map((a) => (
              <AgentCard
                key={a.id}
                agent={a}
                onEdit={setDetailAgent}
                onDelete={setDeletingAgent}
                onResummon={handleResummon}
              />
            ))}
          </div>
        )}
      </div>

      {/* Create dialog */}
      <AgentFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        onSubmit={handleCreate}
      />

      {/* Fullscreen detail panel */}
      {detailAgent && (
        <AgentDetailPanel
          agent={detailAgent}
          onSave={handleSave}
          onResummon={async (id) => { await resummonAgent(id); setSummoningAgent({ id, name: detailAgent.display_name || detailAgent.agent_key }) }}
          onClose={() => setDetailAgent(null)}
        />
      )}

      {/* Delete confirm */}
      <ConfirmDeleteDialog
        open={!!deletingAgent}
        onOpenChange={(open) => { if (!open) setDeletingAgent(null) }}
        title="Delete agent?"
        description="This will permanently delete this agent and all its data."
        confirmValue={deletingAgent?.display_name || deletingAgent?.agent_key || ''}
        confirmLabel="Delete"
        onConfirm={handleDelete}
      />

      {/* Summoning */}
      {summoningAgent && (
        <SummoningModal
          agentId={summoningAgent.id}
          agentName={summoningAgent.name}
          onContinue={handleSummoningComplete}
        />
      )}
    </>
  )
}

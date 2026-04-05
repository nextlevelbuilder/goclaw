import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useChannelCrud } from '../../../hooks/use-channel-crud'
import { useChannelStatus } from '../../../hooks/use-channel-status'
import { useAgentCrud } from '../../../hooks/use-agent-crud'
import { ChannelCard } from './ChannelCard'
import { ChannelFormDialog } from './ChannelFormDialog'
import { ChannelDetailPanel } from './ChannelDetailPanel'
import { PairedDevicesSection } from './PairedDevicesSection'
import { ConfirmDialog } from '../../common/ConfirmDialog'
import { RefreshButton } from '../../common/RefreshButton'
import type { ChannelInstanceData } from '../../../types/channel'

export function ChannelList() {
  const { t } = useTranslation('channels')
  const { instances, loading, atLimit, telegramExists, discordExists, fetchInstances, createInstance, updateInstance, deleteInstance } = useChannelCrud()
  const { statusMap, refreshStatus } = useChannelStatus()
  const { agents } = useAgentCrud()

  const [formOpen, setFormOpen] = useState(false)
  const [detailId, setDetailId] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<ChannelInstanceData | null>(null)

  const refresh = async () => { await fetchInstances(); await refreshStatus() }

  // Detail view replaces list
  if (detailId) {
    const inst = instances.find((i) => i.id === detailId)
    return (
      <ChannelDetailPanel
        instanceId={detailId}
        status={inst ? (statusMap[inst.name] ?? null) : null}
        onBack={() => { setDetailId(null); refresh() }}
        onDelete={() => {
          if (inst) setDeleteTarget(inst)
        }}
      />
    )
  }

  const agentNameMap = new Map(agents.map((a) => [a.id, a.display_name || a.agent_key]))

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">{t('title')}</h2>
          <p className="text-xs text-text-muted mt-0.5">{t('description')}</p>
        </div>
        <div className="flex items-center gap-2">
          <RefreshButton onRefresh={refresh} />
          <button
            onClick={() => setFormOpen(true)}
            disabled={atLimit}
            className="px-3 py-1.5 text-xs bg-accent text-white rounded-lg font-medium hover:bg-accent-hover transition-colors disabled:opacity-50 cursor-pointer"
            title={atLimit ? t('atLimit') : undefined}
          >
            {t('addChannel')}
          </button>
        </div>
      </div>

      {/* Limit warning */}
      {atLimit && (
        <div className="rounded-lg border border-amber-500/30 bg-amber-500/5 px-3 py-2">
          <p className="text-[11px] text-amber-600 dark:text-amber-400">{t('atLimit')}</p>
        </div>
      )}

      {/* Channel cards */}
      {loading && instances.length === 0 ? (
        <div className="space-y-2">
          {[1, 2].map((i) => <div key={i} className="h-16 rounded-xl bg-surface-tertiary/50 animate-pulse" />)}
        </div>
      ) : instances.length === 0 ? (
        <div className="flex flex-col items-center gap-2 py-10">
          <svg className="h-10 w-10 text-text-muted/40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
            <path d="M22 8.35V20a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V8.35A2 2 0 0 1 3.26 6.5l8-3.2a2 2 0 0 1 1.48 0l8 3.2A2 2 0 0 1 22 8.35Z" />
            <path d="M6 18h12" /><path d="M6 14h12" />
          </svg>
          <p className="text-sm text-text-muted">{t('emptyTitle')}</p>
          <p className="text-xs text-text-muted/70">{t('emptyDesc')}</p>
        </div>
      ) : (
        <div className="grid gap-2">
          {instances.map((inst) => (
            <ChannelCard
              key={inst.id}
              instance={inst}
              status={statusMap[inst.name] ?? null}
              agentName={agentNameMap.get(inst.agent_id) ?? inst.agent_id.slice(0, 8)}
              onToggleEnabled={(enabled) => updateInstance(inst.id, { enabled })}
              onClick={() => setDetailId(inst.id)}
            />
          ))}
        </div>
      )}

      {/* Divider */}
      <div className="border-t border-border" />

      {/* Paired devices */}
      <PairedDevicesSection />

      {/* Create dialog */}
      <ChannelFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        agents={agents}
        telegramExists={telegramExists}
        discordExists={discordExists}
        onSubmit={createInstance}
      />

      {/* Delete confirm */}
      {deleteTarget && (
        <ConfirmDialog
          open
          onOpenChange={() => setDeleteTarget(null)}
          title={t('delete.title')}
          description={t('delete.description', { name: deleteTarget.display_name || deleteTarget.name })}
          confirmLabel={t('delete.confirmLabel')}
          variant="destructive"
          onConfirm={async () => {
            await deleteInstance(deleteTarget.id)
            setDeleteTarget(null)
            setDetailId(null)
          }}
        />
      )}
    </div>
  )
}

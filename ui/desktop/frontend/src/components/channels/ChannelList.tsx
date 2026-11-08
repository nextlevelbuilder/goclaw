import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useChannelCrud } from '../../hooks/use-channel-crud'
import { useChannelStatus } from '../../hooks/use-channel-status'
import { useAgentCrud } from '../../hooks/use-agent-crud'
import { useGatewayStatus } from '../layout/use-gateway-status'
import { cn } from '../../lib/cn'
import { Button, ConfirmDialog, EmptyState, ErrorState, IconButton, PageHeader, SkeletonList } from '../ui'
import { IconPlus, IconRefresh } from '../common/Icons'
import { ChannelTable } from './channel-table'
import { ChannelFormDialog } from './ChannelFormDialog'
import { ChannelDetailPanel } from './ChannelDetailPanel'
import { PairedDevicesSection } from './PairedDevicesSection'
import type { ChannelInstanceData } from '../../types/channel'

export function ChannelList() {
  const { t } = useTranslation(['channels', 'common'])
  const { instances, loading, error, atLimit, telegramExists, discordExists, fetchInstances, createInstance, updateInstance, deleteInstance } = useChannelCrud()
  const { statusMap, refreshStatus } = useChannelStatus()
  const { agents } = useAgentCrud()
  const { disconnected } = useGatewayStatus()

  const [formOpen, setFormOpen] = useState(false)
  const [detailId, setDetailId] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<ChannelInstanceData | null>(null)
  const [refreshing, setRefreshing] = useState(false)

  const refresh = async () => {
    setRefreshing(true)
    try { await Promise.all([fetchInstances(), refreshStatus()]) } finally { setRefreshing(false) }
  }

  const name = (i: ChannelInstanceData) => i.display_name || i.name

  const deleteDialog = (
    <ConfirmDialog
      open={deleteTarget !== null}
      onOpenChange={(open) => { if (!open) setDeleteTarget(null) }}
      title={deleteTarget ? t('channels:screen.delete.title', { name: name(deleteTarget) }) : ''}
      description={t('channels:screen.delete.description')}
      confirmLabel={t('channels:delete.confirmLabel')}
      variant="destructive"
      confirmValue={deleteTarget?.name}
      onConfirm={async () => {
        if (!deleteTarget) return
        try {
          await deleteInstance(deleteTarget.id)
          setDeleteTarget(null)
          setDetailId(null)
        } catch { /* reported by the hook */ }
      }}
    />
  )

  // Detail view replaces the list
  if (detailId) {
    const inst = instances.find((i) => i.id === detailId)
    return (
      <>
        <ChannelDetailPanel
          instanceId={detailId}
          status={inst ? (statusMap[inst.name] ?? null) : null}
          onBack={() => { setDetailId(null); void refresh() }}
          onDelete={() => { if (inst) setDeleteTarget(inst) }}
        />
        {deleteDialog}
      </>
    )
  }

  const agentNames = new Map(agents.map((a) => [a.id, a.display_name || a.agent_key]))
  const enabledCount = instances.filter((i) => i.enabled).length

  let body
  if (loading) {
    body = <SkeletonList rows={2} label={t('channels:screen.loading')} />
  } else if (error && instances.length === 0) {
    body = (
      <ErrorState
        title={t('channels:screen.loadError')}
        description={t('channels:screen.loadErrorHint')}
        onRetry={() => { void refresh() }}
        retrying={refreshing}
      />
    )
  } else if (instances.length === 0) {
    body = (
      <EmptyState
        title={t('channels:emptyTitle')}
        description={t('channels:emptyDesc')}
        action={<Button variant="primary" icon={<IconPlus size={14} />} onClick={() => setFormOpen(true)} disabled={disconnected}>{t('channels:addChannel')}</Button>}
      />
    )
  } else {
    body = (
      <ChannelTable
        instances={instances}
        statusMap={statusMap}
        agentNames={agentNames}
        readOnly={disconnected}
        onToggle={(i, enabled) => { void updateInstance(i.id, { enabled }).catch(() => { /* reported by the hook */ }) }}
        onOpen={(i) => setDetailId(i.id)}
        onDelete={setDeleteTarget}
      />
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader
        title={t('channels:title')}
        description={t('channels:screen.description', { count: instances.length, enabled: enabledCount })}
        actions={
          <>
            <IconButton
              aria-label={t('common:refresh')}
              icon={<IconRefresh size={16} />}
              onClick={() => { void refresh() }}
              disabled={refreshing}
            />
            <Button variant="primary" icon={<IconPlus size={14} />} onClick={() => setFormOpen(true)} disabled={atLimit || disconnected}>
              {t('channels:addChannel')}
            </Button>
          </>
        }
      />

      <div className={cn('flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto px-6 py-4', disconnected && 'opacity-60')}>
        {atLimit && (
          <p role="status" className="rounded-md bg-warning-subtle px-3 py-2 text-caption text-warning">
            {t('channels:atLimit')}
          </p>
        )}
        {body}
        <PairedDevicesSection readOnly={disconnected} />
      </div>

      <ChannelFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        agents={agents}
        telegramExists={telegramExists}
        discordExists={discordExists}
        readOnly={disconnected}
        onSubmit={createInstance}
      />

      {deleteDialog}
    </div>
  )
}

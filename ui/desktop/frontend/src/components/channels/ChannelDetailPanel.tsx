import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useChannelDetail } from '../../hooks/use-channel-detail'
import { useAgentCrud } from '../../hooks/use-agent-crud'
import { useGatewayStatus } from '../layout/use-gateway-status'
import { cn } from '../../lib/cn'
import { Button, ErrorState, IconButton, PageHeader, SkeletonList, StatusChip, Tabs, TabsContent, TabsList, TabsTrigger } from '../ui'
import { IconChevronLeft, IconTrash } from '../common/Icons'
import { ChannelGeneralTab } from './ChannelGeneralTab'
import { ChannelCredentialsTab } from './ChannelCredentialsTab'
import { ChannelManagersTab } from './ChannelManagersTab'
import { ChannelAdvancedTab } from './ChannelAdvancedTab'
import { getChannelStatusChip } from '../../utils/channel-status'
import type { ChannelStatus } from '../../types/channel'

interface ChannelDetailPanelProps {
  instanceId: string
  status: ChannelStatus | null
  onBack: () => void
  onDelete: () => void
}

const TABS = ['general', 'credentials', 'managers', 'advanced'] as const
type TabKey = (typeof TABS)[number]

export function ChannelDetailPanel({ instanceId, status, onBack, onDelete }: ChannelDetailPanelProps) {
  const { t } = useTranslation(['channels', 'common'])
  const {
    instance, loading, error, refresh, updateInstance,
    listManagerGroups, listManagers, addManager, removeManager, listContacts,
  } = useChannelDetail(instanceId)
  const { agents } = useAgentCrud()
  const { disconnected } = useGatewayStatus()
  const [activeTab, setActiveTab] = useState<TabKey>('general')

  const backButton = (
    <IconButton aria-label={t('channels:detail.back')} icon={<IconChevronLeft size={16} />} onClick={onBack} />
  )

  if (!instance) {
    return (
      <div className="flex h-full min-h-0 flex-col">
        <PageHeader leading={backButton} title={t('channels:title')} />
        <div className="px-6 py-4">
          {error && !loading ? (
            <ErrorState
              title={t('channels:screen.detail.loadError')}
              description={t('channels:screen.loadErrorHint')}
              onRetry={() => { void refresh() }}
            />
          ) : (
            <SkeletonList rows={3} avatar={false} label={t('channels:screen.loading')} />
          )}
        </div>
      </div>
    )
  }

  const chip = getChannelStatusChip(instance.enabled, status, t as (key: string, options?: Record<string, unknown>) => string)
  const agent = agents.find((a) => a.id === instance.agent_id)
  const agentName = agent?.display_name || agent?.agent_key || instance.agent_id.slice(0, 8)
  const typeName = t(`channels:channelTypes.${instance.channel_type}`, { defaultValue: instance.channel_type })

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader
        leading={backButton}
        title={instance.display_name || instance.name}
        description={`${typeName} · ${agentName}`}
        actions={
          <>
            <StatusChip tone={chip.tone} label={chip.label} />
            <Button variant="danger" icon={<IconTrash size={14} />} onClick={onDelete} disabled={disconnected}>
              {t('channels:delete.confirmLabel')}
            </Button>
          </>
        }
      />

      <div className={cn('flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-6 py-4', disconnected && 'opacity-60')}>
        {(status?.summary || status?.detail) && (
          <div className="flex flex-col gap-0.5 rounded-lg border border-border-subtle bg-surface-base px-4 py-3">
            {status.summary && <p className="break-words text-ui text-ink">{status.summary}</p>}
            {status.detail && <p className="break-words text-caption text-ink-muted">{status.detail}</p>}
            {status.checked_at && (
              <p className="text-caption text-ink-muted">
                {t('channels:detail.lastChecked', { value: new Date(status.checked_at).toLocaleString() })}
              </p>
            )}
          </div>
        )}

        <Tabs value={activeTab} onValueChange={(v) => setActiveTab(v as TabKey)} className="gap-4">
          <TabsList aria-label={t('channels:detail.tabsLabel')}>
            {TABS.map((tab) => (
              <TabsTrigger key={tab} value={tab}>{t(`channels:detail.tabs.${tab}`)}</TabsTrigger>
            ))}
          </TabsList>

          <TabsContent value="general">
            <ChannelGeneralTab instance={instance} agents={agents} readOnly={disconnected} onUpdate={updateInstance} />
          </TabsContent>
          <TabsContent value="credentials">
            <fieldset disabled={disconnected} className="m-0 min-w-0 border-0 p-0">
              <ChannelCredentialsTab instance={instance} onUpdate={updateInstance} />
            </fieldset>
          </TabsContent>
          <TabsContent value="managers">
            <ChannelManagersTab
              readOnly={disconnected}
              listManagerGroups={listManagerGroups}
              listManagers={listManagers}
              addManager={addManager}
              removeManager={removeManager}
              listContacts={listContacts}
            />
          </TabsContent>
          <TabsContent value="advanced">
            <ChannelAdvancedTab instance={instance} readOnly={disconnected} onUpdate={updateInstance} />
          </TabsContent>
        </Tabs>
      </div>
    </div>
  )
}

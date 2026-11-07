import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PROVIDER_TYPES } from '../../constants/providers'
import { useProviderHealth } from '../../hooks/use-provider-health'
import { useProviderUsage } from '../../hooks/use-provider-usage'
import { useGatewayStatus } from '../layout/use-gateway-status'
import { cn } from '../../lib/cn'
import { IconChevronLeft } from '../common/Icons'
import { Badge, IconButton, PageHeader, SegmentedControl } from '../ui'
import { ProviderConnectionTab } from './ProviderConnectionTab'
import { ProviderUsageTab } from './ProviderUsageTab'
import { ProviderStatusChip } from './provider-usage-widgets'
import { providerStatus } from './provider-usage-format'
import type { ProviderData, UsageRange } from '../../types/provider'

const TABS = ['connection', 'usage'] as const
type TabKey = (typeof TABS)[number]
const RANGES: UsageRange[] = ['24h', '7d', '30d']

interface ProviderDetailProps {
  provider: ProviderData
  onBack: () => void
  onEdit: (provider: ProviderData) => void
}

/**
 * Provider detail view with Connection and Usage tabs.
 * Usage data is cached per range for as long as this view stays open.
 */
export function ProviderDetail({ provider, onBack, onEdit }: ProviderDetailProps) {
  const { t } = useTranslation('providers')
  const { disconnected } = useGatewayStatus()
  const [activeTab, setActiveTab] = useState<TabKey>('connection')
  const [range, setRange] = useState<UsageRange>('7d')
  const health = useProviderHealth(provider.id)
  const usage = useProviderUsage(provider.id, provider.name, range, activeTab === 'usage')
  const typeLabel = PROVIDER_TYPES.find((p) => p.value === provider.provider_type)?.label ?? provider.provider_type

  const retry = () => {
    usage.refetch()
    void health.refetch()
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        leading={<IconButton aria-label={t('providerDetail.back')} icon={<IconChevronLeft size={16} />} onClick={onBack} />}
        title={(
          <span className="flex min-w-0 items-center gap-2">
            <span className="truncate">{provider.display_name || provider.name}</span>
            <Badge>{typeLabel}</Badge>
            <ProviderStatusChip status={providerStatus(provider.enabled, health.health)} />
          </span>
        )}
      />

      <div className="flex shrink-0 flex-wrap items-end justify-between gap-2 border-b border-border-subtle px-6">
        <div role="tablist" className="flex gap-1">
          {TABS.map((tab) => (
            <button
              key={tab}
              type="button"
              role="tab"
              aria-selected={activeTab === tab}
              onClick={() => setActiveTab(tab)}
              className={cn(
                '-mb-px inline-flex h-control items-center border-b-2 px-3 text-ui transition-colors',
                activeTab === tab
                  ? 'border-accent text-ink'
                  : 'border-transparent text-ink-secondary hover:text-ink',
              )}
            >
              {t(`providerDetail.tabs.${tab}`)}
            </button>
          ))}
        </div>
        {activeTab === 'usage' && (
          <SegmentedControl
            aria-label={t('usage.range.label')}
            size="sm"
            value={range}
            onValueChange={setRange}
            options={RANGES.map((r) => ({ value: r, label: t(`usage.range.${r}`) }))}
            className="mb-1.5"
          />
        )}
      </div>

      <div role="tabpanel" className={cn('min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-4', disconnected && 'opacity-60')}>
        {activeTab === 'connection' ? (
          <ProviderConnectionTab provider={provider} onEdit={() => onEdit(provider)} disabled={disconnected} />
        ) : (
          <ProviderUsageTab range={range} usage={usage} health={health.health} onRetry={retry} />
        )}
      </div>
    </div>
  )
}

import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { PROVIDER_TYPES } from '../../constants/providers'
import { useProviderHealth } from '../../hooks/use-provider-health'
import { useProviderUsage } from '../../hooks/use-provider-usage'
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
 * Provider detail view with Connection and Usage tabs. Rendered inside the settings
 * content area, which already sits below the window's 32px drag overlay.
 * Usage data is cached per range for as long as this view stays open.
 */
export function ProviderDetail({ provider, onBack, onEdit }: ProviderDetailProps) {
  const { t } = useTranslation('providers')
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
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center gap-3 min-w-0">
        <button
          onClick={onBack}
          className="text-xs text-text-muted hover:text-text-primary transition-colors shrink-0 cursor-pointer"
        >
          ← {t('providerDetail.back')}
        </button>
        <h3 className="text-sm font-semibold text-text-primary truncate">{provider.display_name || provider.name}</h3>
        <span className="text-2xs px-1.5 py-0.5 rounded bg-surface-tertiary text-text-muted shrink-0">{typeLabel}</span>
        <ProviderStatusChip status={providerStatus(provider.enabled, health.health)} />
      </div>

      {/* Tabs + range */}
      <div className="flex flex-wrap items-end justify-between gap-2 border-b border-border">
        <div role="tablist" className="flex gap-1">
          {TABS.map((tab) => (
            <button
              key={tab}
              role="tab"
              aria-selected={activeTab === tab}
              onClick={() => setActiveTab(tab)}
              className={`px-3 py-1.5 text-xs transition-colors cursor-pointer ${
                activeTab === tab
                  ? 'text-accent font-medium border-b-2 border-accent -mb-px'
                  : 'text-text-muted hover:text-text-primary'
              }`}
            >
              {t(`providerDetail.tabs.${tab}`)}
            </button>
          ))}
        </div>
        {activeTab === 'usage' && (
          <div role="radiogroup" aria-label={t('usage.range.label')} className="flex mb-1.5 rounded-lg border border-border p-0.5">
            {RANGES.map((r) => (
              <button
                key={r}
                role="radio"
                aria-checked={range === r}
                onClick={() => setRange(r)}
                className={`px-2.5 py-1 text-2xs rounded-md transition-colors cursor-pointer ${
                  range === r ? 'bg-surface-tertiary text-text-primary font-medium' : 'text-text-muted hover:text-text-primary'
                }`}
              >
                {t(`usage.range.${r}`)}
              </button>
            ))}
          </div>
        )}
      </div>

      <div role="tabpanel">
        {activeTab === 'connection' ? (
          <ProviderConnectionTab provider={provider} onEdit={() => onEdit(provider)} />
        ) : (
          <ProviderUsageTab range={range} usage={usage} health={health.health} onRetry={retry} />
        )}
      </div>
    </div>
  )
}

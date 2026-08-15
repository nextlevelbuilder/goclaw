import { useTranslation } from 'react-i18next'
import type { SettingsTab } from '../../stores/ui-store'

const TAB_KEYS: SettingsTab[] = [
  'appearance', 'providers', 'agents', 'channels', 'mcp', 'skills', 'tools', 'cron', 'traces', 'storage', 'about',
]

interface SettingsTabBarProps {
  activeTab: SettingsTab
  onTabChange: (tab: SettingsTab) => void
}

export function SettingsTabBar({ activeTab, onTabChange }: SettingsTabBarProps) {
  const { t } = useTranslation('desktop')
  return (
    <div className="flex gap-1 overflow-x-auto px-1 pb-1 border-b border-border">
      {TAB_KEYS.map((key) => (
        <button
          key={key}
          onClick={() => onTabChange(key)}
          className={[
            'shrink-0 px-3 py-1.5 text-xs rounded-md transition-colors',
            activeTab === key
              ? 'bg-accent/10 text-accent font-medium'
              : 'text-text-muted hover:text-text-primary hover:bg-surface-tertiary',
          ].join(' ')}
        >
          {t(`settings.tabs.${key}`)}
        </button>
      ))}
    </div>
  )
}

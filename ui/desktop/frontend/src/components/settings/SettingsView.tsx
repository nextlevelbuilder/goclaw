import { useUiStore } from '../../stores/ui-store'
import { SettingsTabBar } from './SettingsTabBar'
import { AppearanceTab } from './AppearanceTab'
import { AboutTab } from './AboutTab'
import { ProviderList } from '../providers/ProviderList'
import { AgentList } from '../agents/AgentList'
import { McpServerList } from '../mcp/McpServerList'
import { SkillList } from '../skills/SkillList'
import { ToolList } from '../tools/ToolList'
import { CronList } from '../cron/CronList'
import { TraceList } from '../traces/TraceList'
import { StorageTab } from '../storage/StorageTab'
import { ChannelList } from '../channels/ChannelList'

export function SettingsView() {
  const settingsTab = useUiStore((s) => s.settingsTab)
  const setSettingsTab = useUiStore((s) => s.setSettingsTab)
  const closeSettings = useUiStore((s) => s.closeSettings)

  return (
    <div className="flex-1 flex flex-col min-h-0">
      {/* Header */}
      <div className="flex items-center justify-between px-4 py-3 border-b border-border">
        <h1 className="text-sm font-semibold text-text-primary">Settings</h1>
        <button
          onClick={closeSettings}
          className="w-6 h-6 flex items-center justify-center rounded text-text-muted hover:text-text-primary hover:bg-surface-tertiary transition-colors"
          title="Close settings (Esc)"
        >
          <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
            <line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" />
          </svg>
        </button>
      </div>

      {/* Tab bar */}
      <div className="px-3 pt-2">
        <SettingsTabBar activeTab={settingsTab} onTabChange={setSettingsTab} />
      </div>

      {/* Tab content */}
      {settingsTab === 'storage' ? (
        <div className="flex-1 flex flex-col min-h-0 px-4 py-4 canvas-dots">
          <div className="bg-surface-secondary border border-border rounded-xl p-5 flex-1 flex flex-col min-h-0">
            <TabContent tab={settingsTab} />
          </div>
        </div>
      ) : (
        <div className="flex-1 overflow-y-auto overscroll-contain px-4 py-4 canvas-dots">
          <div className="bg-surface-secondary border border-border rounded-xl p-5">
            <TabContent tab={settingsTab} />
          </div>
        </div>
      )}
    </div>
  )
}

function TabContent({ tab }: { tab: string }) {
  switch (tab) {
    case 'appearance': return <AppearanceTab />
    case 'providers': return <ProviderList />
    case 'agents': return <AgentList />
    case 'channels': return <ChannelList />
    case 'mcp': return <McpServerList />
    case 'skills': return <SkillList />
    case 'tools': return <ToolList />
    case 'cron': return <CronList />
    case 'traces': return <TraceList />
    case 'storage': return <StorageTab />
    case 'about': return <AboutTab />
    default:
      return (
        <div className="flex flex-col items-center justify-center py-16 text-center">
          <p className="text-sm text-text-muted">Coming soon</p>
          <p className="text-xs text-text-muted mt-1">This tab will be available in a future update.</p>
        </div>
      )
  }
}

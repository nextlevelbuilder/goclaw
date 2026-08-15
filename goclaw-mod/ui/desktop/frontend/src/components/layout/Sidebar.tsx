import { SidebarHeader } from './sidebar/SidebarHeader'
import { SidebarTeams } from './sidebar/SidebarTeams'
import { SessionList } from './sidebar/SessionList'
import { SidebarFooter } from './sidebar/SidebarFooter'

export function Sidebar() {
  return (
    <aside className="flex flex-col h-full overflow-hidden">
      <SidebarHeader />

      {/* Divider */}
      <div className="mx-3 border-t border-border" />

      {/* Teams section */}
      <SidebarTeams />

      {/* Session list — scrollable */}
      <div className="flex-1 overflow-y-auto overscroll-contain py-2">
        <SessionList />
      </div>

      {/* Divider */}
      <div className="mx-3 border-t border-border" />

      <SidebarFooter />
    </aside>
  )
}

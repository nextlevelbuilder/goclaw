import { Suspense, useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { usePendingPairingsCount } from '../../hooks/use-pending-pairings-count'
import { useSessions } from '../../hooks/use-sessions'
import { cn } from '../../lib/cn'
import { useUiStore, type AppView } from '../../stores/ui-store'
import { ErrorBoundary } from '../common/ErrorBoundary'
import { ErrorState, OfflineBanner, SkeletonList } from '../ui'
import { CommandPalette } from './CommandPalette'
import { startNewChat } from './shell-actions'
import { Sidebar } from './Sidebar'
import { Titlebar } from './Titlebar'
import { UpdateBanner } from './UpdateBanner'
import { useGatewayStatus } from './use-gateway-status'
import { useNarrowWindow } from './use-narrow-window'
import { VIEW_REGISTRY } from './view-registry'

const panelClass = 'rounded-xl border border-border-subtle bg-surface-base shadow-panel'

/** Renders the active view inside the main panel with the frame its registry entry asks for. */
function ViewOutlet({ view }: { view: AppView }) {
  const { t } = useTranslation('common')
  const def = VIEW_REGISTRY[view]
  const View = def.component
  const fallback = def.frame === 'page' ? <SkeletonList className="px-6 py-5" /> : <SkeletonList className="p-6" />
  const content = (
    <ErrorBoundary
      key={view}
      fallback={<ErrorState description={t('errorBoundary')} onRetry={() => window.location.reload()} />}
    >
      <Suspense fallback={fallback}>
        <View />
      </Suspense>
    </ErrorBoundary>
  )

  switch (def.frame) {
    case 'page':
      return <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-5">{content}</div>
    case 'fill-padded':
      return <div className="flex min-h-0 flex-1 flex-col px-6 py-5">{content}</div>
    default:
      return <div className="flex min-h-0 flex-1 flex-col">{content}</div>
  }
}

/** True when a modal dialog (Radix or legacy) is open, so global shortcuts stay out of its way. */
function dialogOpen(): boolean {
  return document.querySelector('[role="dialog"], [role="alertdialog"]') !== null
}

/**
 * Window shell: custom title bar, sidebar panel (full, or icon rail below
 * 1100px / when collapsed) and the main panel with update and offline banners.
 * Owns the global shortcuts: Ctrl+K palette, Ctrl+B sidebar, Ctrl+N new chat,
 * Ctrl+, settings, Esc closes overlays / settings.
 */
export function AppShell() {
  const { t } = useTranslation('desktop')
  const sidebarOpen = useUiStore((s) => s.sidebarOpen)
  const toggleSidebar = useUiStore((s) => s.toggleSidebar)
  const activeView = useUiStore((s) => s.activeView)
  const narrow = useNarrowWindow()
  const [overlayOpen, setOverlayOpen] = useState(false)
  const gateway = useGatewayStatus()
  const { deleteSession } = useSessions() // the single instance: loads sessions for the selected agent
  const { pendingCount } = usePendingPairingsCount()
  const badges = { channels: pendingCount }

  const rail = narrow || !sidebarOpen
  const closeOverlay = useCallback(() => setOverlayOpen(false), [])

  // Leaving narrow mode closes the overlay.
  useEffect(() => { if (!narrow) setOverlayOpen(false) }, [narrow])

  const toggleNav = useCallback(() => {
    if (narrow) setOverlayOpen((o) => !o)
    else toggleSidebar()
  }, [narrow, toggleSidebar])

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.ctrlKey || e.metaKey
      const key = e.key.toLowerCase()
      const ui = useUiStore.getState()
      if (mod && key === 'k') {
        e.preventDefault()
        ui.setCommandPaletteOpen(!ui.commandPaletteOpen)
        return
      }
      if (ui.commandPaletteOpen) return
      if (mod && key === 'b') { e.preventDefault(); toggleNav(); return }
      if (mod && key === 'n') { e.preventDefault(); startNewChat(); setOverlayOpen(false); return }
      if (mod && e.key === ',') { e.preventDefault(); ui.openSettings('appearance'); return }
      if (e.key === 'Escape' && !e.defaultPrevented && !dialogOpen()) {
        if (overlayOpen) setOverlayOpen(false)
        else if (ui.activeView === 'settings') ui.closeSettings()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [toggleNav, overlayOpen])

  return (
    <div className="flex h-dvh flex-col overflow-hidden bg-surface-window text-ink">
      <Titlebar />
      <div className="relative flex min-h-0 flex-1 gap-3 px-3 pb-3">
        <aside
          data-mode={rail ? 'rail' : 'full'}
          className={cn(panelClass, 'shrink-0 overflow-hidden transition-[width] duration-200 ease-out', rail ? 'w-rail' : 'w-sidebar')}
        >
          {rail ? (
            <Sidebar
              mode="rail"
              badges={badges}
              onDeleteSession={deleteSession}
              onExpand={narrow ? () => setOverlayOpen(true) : toggleSidebar}
            />
          ) : (
            <Sidebar
              mode="full"
              badges={badges}
              onDeleteSession={deleteSession}
              onCollapse={toggleSidebar}
              collapseLabel={t('nav.collapse')}
            />
          )}
        </aside>

        {narrow && overlayOpen && (
          <>
            <div aria-hidden="true" data-testid="sidebar-overlay-backdrop" className="absolute inset-0 z-30" onClick={closeOverlay} />
            <aside
              data-mode="overlay"
              className="absolute bottom-3 left-3 top-0 z-40 w-sidebar overflow-hidden rounded-xl border border-border-subtle bg-surface-base shadow-popover"
            >
              <Sidebar
                mode="full"
                badges={badges}
                onDeleteSession={deleteSession}
                onCollapse={closeOverlay}
                collapseLabel={t('nav.hide')}
                onNavigate={closeOverlay}
              />
            </aside>
          </>
        )}

        <main className={cn(panelClass, 'flex min-w-0 flex-1 flex-col overflow-hidden')}>
          <UpdateBanner />
          {gateway.disconnected && (
            <div className="shrink-0 px-6 pt-4">
              <OfflineBanner
                retryInSeconds={gateway.retryInSeconds}
                retrying={gateway.status === 'connecting'}
                onRetry={gateway.retryNow}
              />
            </div>
          )}
          <ViewOutlet view={activeView} />
        </main>
      </div>
      <CommandPalette />
    </div>
  )
}

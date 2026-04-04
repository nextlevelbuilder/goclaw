import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useSessions } from '../../../hooks/use-sessions'
import { useUiStore } from '../../../stores/ui-store'
import { getWsClient } from '../../../lib/ws'
import { usePendingPairingsCount } from '../../../hooks/use-pending-pairings-count'

export function SidebarFooter() {
  const { t } = useTranslation('desktop')
  const { createSession } = useSessions()
  const openSettings = useUiStore((s) => s.openSettings)
  const closeSettings = useUiStore((s) => s.closeSettings)
  const toggleTheme = useUiStore((s) => s.toggleTheme)
  const theme = useUiStore((s) => s.theme)

  const { pendingCount } = usePendingPairingsCount()

  const [connected, setConnected] = useState(() => {
    try { return getWsClient().isConnected } catch { return false }
  })

  useEffect(() => {
    let ws: ReturnType<typeof getWsClient>
    try { ws = getWsClient() } catch { return }
    ws.onConnectionChange(setConnected)
    return () => { ws.onConnectionChange(() => {}) }
  }, [])

  return (
    <div className="p-3 space-y-2">
      <button
        onClick={() => { createSession(); closeSettings() }}
        className="wails-no-drag w-full py-2 px-3 rounded-lg bg-accent text-white text-sm font-medium text-center hover:bg-accent-hover transition-colors"
      >
        {t('sidebar.newChat')}
      </button>

      <div className="flex items-center justify-between px-1">
        {/* Connection status */}
        <div className="flex items-center gap-1.5">
          <span className={`w-1.5 h-1.5 rounded-full ${connected ? 'bg-success' : 'bg-error'}`} />
          <span className="text-[10px] text-text-muted">{connected ? t('sidebar.connected') : t('sidebar.offline')}</span>
        </div>

        {/* Action buttons */}
        <div className="wails-no-drag flex items-center gap-1">
          {/* Theme toggle */}
          <button
            onClick={toggleTheme}
            className="w-6 h-6 flex items-center justify-center rounded text-text-muted hover:text-text-primary hover:bg-surface-tertiary transition-colors"
            title={theme === 'dark' ? t('sidebar.lightMode') : t('sidebar.darkMode')}
          >
            {theme === 'dark' ? (
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><circle cx="12" cy="12" r="5" /><line x1="12" y1="1" x2="12" y2="3" /><line x1="12" y1="21" x2="12" y2="23" /><line x1="4.22" y1="4.22" x2="5.64" y2="5.64" /><line x1="18.36" y1="18.36" x2="19.78" y2="19.78" /><line x1="1" y1="12" x2="3" y2="12" /><line x1="21" y1="12" x2="23" y2="12" /><line x1="4.22" y1="19.78" x2="5.64" y2="18.36" /><line x1="18.36" y1="5.64" x2="19.78" y2="4.22" /></svg>
            ) : (
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"><path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" /></svg>
            )}
          </button>

          {/* Pairing notification */}
          {pendingCount > 0 && (
            <button
              onClick={() => openSettings('channels')}
              className="relative w-6 h-6 flex items-center justify-center rounded text-text-muted hover:text-text-primary hover:bg-surface-tertiary transition-colors"
              title={`${pendingCount} pending pairing request(s)`}
            >
              <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
                <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
              </svg>
              <span className="absolute -top-0.5 -right-0.5 w-3.5 h-3.5 rounded-full bg-amber-500 text-[8px] text-white font-bold flex items-center justify-center">
                {pendingCount}
              </span>
            </button>
          )}

          {/* Settings */}
          <button
            onClick={() => openSettings()}
            className="w-6 h-6 flex items-center justify-center rounded text-text-muted hover:text-text-primary hover:bg-surface-tertiary transition-colors"
            title={`${t('sidebar.settings')} (⌘,)`}
          >
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <circle cx="12" cy="12" r="3" />
              <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
            </svg>
          </button>
        </div>
      </div>
    </div>
  )
}

import './i18n'
import { useEffect, useState } from 'react'
import { useUiStore } from './stores/ui-store'
import { AppShell } from './components/layout/AppShell'
import { ChatCanvas } from './components/chat/ChatCanvas'
import { OnboardingWizard } from './components/onboarding/OnboardingWizard'
import { wails } from './lib/wails'
import { initWsClient } from './lib/ws'
import { initApiClient } from './lib/api'
import { useSessionStore } from './stores/session-store'
import { useChatMessageStore } from './stores/chat-message-store'
import { useChatActivityStore } from './stores/chat-activity-store'
import { ErrorBoundary } from './components/common/ErrorBoundary'
import { Toaster } from './components/common/Toaster'
import { SplashScreen } from './components/common/SplashScreen'

function AppReady() {
  const toggleSidebar = useUiStore((s) => s.toggleSidebar)
  const openSettings = useUiStore((s) => s.openSettings)
  const closeSettings = useUiStore((s) => s.closeSettings)
  const activeView = useUiStore((s) => s.activeView)

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey
      if (mod && e.key === 'b') { e.preventDefault(); toggleSidebar() }
      if (mod && e.key === 'n') {
        e.preventDefault()
        // "New Chat" — clear session + chat directly (avoids duplicate useSessions instance)
        useSessionStore.getState().setActiveSession(null)
        useChatMessageStore.getState().clear()
        useChatActivityStore.getState().clear()
      }
      if (mod && e.key === ',') { e.preventDefault(); openSettings() }
      if (e.key === 'Escape' && activeView === 'settings') { closeSettings() }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [toggleSidebar, openSettings, closeSettings, activeView])

  return (
    <>
      <AppShell>
        <ChatCanvas />
      </AppShell>
      <Toaster />
    </>
  )
}

function App() {
  const theme = useUiStore((s) => s.theme)
  const onboarded = useUiStore((s) => s.onboarded)
  const completeOnboarding = useUiStore((s) => s.completeOnboarding)
  const [ready, setReady] = useState(false)
  const [splashDone, setSplashDone] = useState(false)

  useEffect(() => {
    document.documentElement.classList.toggle('dark', theme === 'dark')
  }, [theme])

  useEffect(() => {
    const splashMin = new Promise((r) => setTimeout(r, 2500))

    const init = async () => {
      let attempts = 0
      while (attempts < 30) {
        try {
          const isReady = await wails.isGatewayReady()
          if (isReady) break
        } catch { /* not ready yet */ }
        await new Promise((r) => setTimeout(r, 500))
        attempts++
      }

      let token = ''
      try { token = await wails.getGatewayToken() } catch (e) {
        console.warn('[app] failed to get token:', e)
      }

      const gatewayUrl = await wails.getGatewayURL()
      const wsUrl = gatewayUrl.replace(/^http/, 'ws') + '/ws'

      initWsClient(wsUrl, token)
      const api = initApiClient(gatewayUrl, token)
      setReady(true)

      // Auto-detect empty DB → reset onboarded flag (handles DB deletion)
      try {
        const [pRes, aRes] = await Promise.allSettled([
          api.get<{ providers?: unknown[] | null }>('/v1/providers'),
          api.get<{ agents?: unknown[] | null }>('/v1/agents'),
        ])
        const hasProviders = pRes.status === 'fulfilled' && (pRes.value.providers?.length ?? 0) > 0
        const hasAgents = aRes.status === 'fulfilled' && (aRes.value.agents?.length ?? 0) > 0
        if (!hasProviders && !hasAgents) {
          useUiStore.getState().resetOnboarding()
        }
      } catch { /* ignore — onboarding wizard will handle */ }
    }

    // Wait for both gateway init AND minimum splash duration
    Promise.all([init(), splashMin]).then(() => setSplashDone(true))
  }, [])

  if (!splashDone) {
    return <SplashScreen ready={ready} />
  }

  if (!onboarded) {
    return <OnboardingWizard onComplete={completeOnboarding} />
  }

  return (
    <ErrorBoundary>
      <AppReady />
    </ErrorBoundary>
  )
}

export default App

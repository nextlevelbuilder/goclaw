import './i18n'
import { useEffect, useState } from 'react'
import { useUiStore } from './stores/ui-store'
import { AppShell } from './components/layout/AppShell'
import { Titlebar } from './components/layout/Titlebar'
import { useResolvedTheme } from './components/layout/use-resolved-theme'
import { applyTheme } from './lib/theme'
import { OnboardingWizard } from './components/onboarding/OnboardingWizard'
import { wails } from './lib/wails'
import { initWsClient } from './lib/ws'
import { initApiClient } from './lib/api'
import { ErrorBoundary } from './components/common/ErrorBoundary'
import { Toaster } from './components/ui/Toast'
import { SplashScreen } from './components/common/SplashScreen'

function AppReady() {
  return (
    <>
      <AppShell />
      <Toaster />
    </>
  )
}

function App() {
  const resolvedTheme = useResolvedTheme()
  const onboarded = useUiStore((s) => s.onboarded)
  const completeOnboarding = useUiStore((s) => s.completeOnboarding)
  const [ready, setReady] = useState(false)
  const [splashDone, setSplashDone] = useState(false)

  useEffect(() => {
    applyTheme(resolvedTheme)
  }, [resolvedTheme])

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

  // The window is frameless: screens outside the shell still need a drag
  // region and window controls.
  if (!splashDone) {
    return (
      <>
        <Titlebar minimal className="fixed inset-x-0 top-0 z-50" />
        <SplashScreen ready={ready} />
      </>
    )
  }

  if (!onboarded) {
    return (
      <>
        <Titlebar minimal className="fixed inset-x-0 top-0 z-50" />
        <OnboardingWizard onComplete={completeOnboarding} />
      </>
    )
  }

  return (
    <ErrorBoundary>
      <AppReady />
    </ErrorBoundary>
  )
}

export default App

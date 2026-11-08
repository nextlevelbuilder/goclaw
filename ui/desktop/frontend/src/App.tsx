import './i18n'
import { useEffect, useState } from 'react'
import { useUiStore } from './stores/ui-store'
import { AppShell } from './components/layout/AppShell'
import { Titlebar } from './components/layout/Titlebar'
import { useResolvedTheme } from './components/layout/use-resolved-theme'
import { applyTheme } from './lib/theme'
import { OnboardingWizard } from './components/onboarding/OnboardingWizard'
import { useGatewayBoot } from './hooks/use-gateway-boot'
import { ErrorBoundary } from './components/common/ErrorBoundary'
import { Toaster } from './components/ui/Toast'
import { SplashScreen, StartFailedScreen } from './components/common/SplashScreen'

/** Shortest time the Starting card stays up, so a fast boot does not flash it. */
const SPLASH_MIN_MS = 800

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
  const { state, retry } = useGatewayBoot()
  const [minElapsed, setMinElapsed] = useState(false)

  useEffect(() => {
    applyTheme(resolvedTheme)
  }, [resolvedTheme])

  useEffect(() => {
    const id = setTimeout(() => setMinElapsed(true), SPLASH_MIN_MS)
    return () => clearTimeout(id)
  }, [])

  // The window is frameless: screens outside the shell still need a drag
  // region and window controls.
  const bar = <Titlebar minimal className="fixed inset-x-0 top-0 z-50" />

  if (state.status === 'failed') {
    return (
      <>
        {bar}
        <StartFailedScreen reason={state.reason} detail={state.detail} onRetry={retry} />
      </>
    )
  }

  if (state.status === 'starting' || !minElapsed) {
    return (
      <>
        {bar}
        <SplashScreen phase={state.status === 'starting' ? state.phase : 'setup'} />
      </>
    )
  }

  if (!onboarded) {
    return (
      <>
        {bar}
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

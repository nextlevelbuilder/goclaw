import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useBootstrapStatus, type SetupStep } from '../../hooks/use-bootstrap-status'
import { useUiStore } from '../../stores/ui-store'
import { ErrorState, Skeleton } from '../ui'
import { SegmentedControl } from '../ui'
import { SetupStepper } from './SetupStepper'
import { ProviderStep } from './ProviderStep'
import { ModelVerifyStep } from './ModelVerifyStep'
import { AgentStep } from './AgentStep'
import type { ProviderData } from '../../types/provider'

const LANGUAGES = [
  { value: 'en', label: 'EN' },
  { value: 'vi', label: 'VI' },
  { value: 'zh', label: '中文' },
  { value: 'ru', label: 'RU' },
]

interface OnboardingWizardProps {
  onComplete: () => void
}

function Frame({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-dvh flex-col items-center justify-center gap-6 overflow-y-auto bg-surface-window px-4 pb-8 pt-titlebar">
      {children}
    </div>
  )
}

export function OnboardingWizard({ onComplete }: OnboardingWizardProps) {
  const { currentStep, loading, error, reload, providers } = useBootstrapStatus()
  const locale = useUiStore((s) => s.locale)
  const setLocale = useUiStore((s) => s.setLocale)
  const { t, i18n } = useTranslation('onboarding')
  const [step, setStep] = useState<1 | 2 | 3>(1)
  const [createdProvider, setCreatedProvider] = useState<ProviderData | null>(null)
  const [selectedModel, setSelectedModel] = useState<string | null>(null)
  const [initialized, setInitialized] = useState(false)

  // Initialize step from server state (only on first load)
  useEffect(() => {
    if (loading || error || initialized) return
    if (currentStep === ('complete' as SetupStep)) {
      onComplete()
      return
    }
    setStep(currentStep as 1 | 2 | 3)
    setInitialized(true)
  }, [currentStep, loading, error, initialized, onComplete])

  if (error) {
    return (
      <Frame>
        <ErrorState
          title={t('loadErrorTitle')}
          description={t('loadErrorBody')}
          onRetry={() => { void reload() }}
          retrying={loading}
        />
      </Frame>
    )
  }

  if (loading || !initialized) {
    return (
      <Frame>
        <div role="status" aria-busy="true" aria-label={t('steps.label')} className="flex w-full max-w-[560px] flex-col gap-4 rounded-xl border border-border-subtle bg-surface-base p-8 shadow-panel">
          <Skeleton className="h-6 w-2/3" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
        </div>
      </Frame>
    )
  }

  const completedSteps: number[] = []
  if (step > 1) completedSteps.push(1)
  if (step > 2) completedSteps.push(2)

  // Resume: find existing provider from server data (any enabled provider)
  const activeProvider = createdProvider ?? providers.find((p) => p.enabled) ?? providers[0] ?? null

  return (
    <Frame>
      <SegmentedControl
        size="sm"
        aria-label={t('language')}
        value={locale}
        onValueChange={(lang) => { setLocale(lang); void i18n.changeLanguage(lang) }}
        options={LANGUAGES}
      />

      <SetupStepper currentStep={step} completedSteps={completedSteps} />

      <div className="w-full max-w-[560px] rounded-xl border border-border-subtle bg-surface-base p-8 shadow-panel">
        {step === 1 && (
          <ProviderStep
            existingProvider={activeProvider}
            onComplete={(provider) => {
              setCreatedProvider(provider)
              setStep(2)
            }}
          />
        )}

        {step === 2 && activeProvider && (
          <ModelVerifyStep
            provider={activeProvider}
            initialModel={selectedModel}
            onBack={() => setStep(1)}
            onComplete={(model) => {
              setSelectedModel(model)
              setStep(3)
            }}
          />
        )}

        {step === 3 && activeProvider && (
          <AgentStep
            provider={activeProvider}
            model={selectedModel}
            onBack={() => setStep(2)}
            onComplete={onComplete}
          />
        )}
      </div>
    </Frame>
  )
}

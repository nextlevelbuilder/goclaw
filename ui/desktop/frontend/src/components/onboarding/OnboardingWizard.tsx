import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { useBootstrapStatus, type SetupStep } from '../../hooks/use-bootstrap-status'
import { useUiStore } from '../../stores/ui-store'
import { SetupStepper } from './SetupStepper'
import { ProviderStep } from './ProviderStep'
import { ModelVerifyStep } from './ModelVerifyStep'
import { AgentStep } from './AgentStep'
import type { ProviderData } from '../../types/provider'

const LANGUAGES = [
  { value: 'en', label: 'EN' },
  { value: 'vi', label: 'VI' },
  { value: 'zh', label: '中文' },
] as const

interface OnboardingWizardProps {
  onComplete: () => void
}

export function OnboardingWizard({ onComplete }: OnboardingWizardProps) {
  const { currentStep, loading, providers } = useBootstrapStatus()
  const locale = useUiStore((s) => s.locale)
  const setLocale = useUiStore((s) => s.setLocale)
  const { t, i18n } = useTranslation('desktop')
  const [step, setStep] = useState<1 | 2 | 3>(1)
  const [createdProvider, setCreatedProvider] = useState<ProviderData | null>(null)
  const [selectedModel, setSelectedModel] = useState<string | null>(null)
  const [initialized, setInitialized] = useState(false)

  // Initialize step from server state (only on first load)
  useEffect(() => {
    if (loading || initialized) return
    if (currentStep === ('complete' as SetupStep)) {
      onComplete()
      return
    }
    setStep(currentStep as 1 | 2 | 3)
    setInitialized(true)
  }, [currentStep, loading, initialized, onComplete])

  if (loading || !initialized) {
    return (
      <div className="h-dvh flex items-center justify-center bg-surface-primary">
        <div className="w-6 h-6 border-2 border-accent border-t-transparent rounded-full animate-spin" />
      </div>
    )
  }

  const completedSteps: number[] = []
  if (step > 1) completedSteps.push(1)
  if (step > 2) completedSteps.push(2)

  // Resume: find existing provider from server data (any enabled provider)
  const activeProvider = createdProvider ?? providers.find((p) => p.enabled) ?? providers[0] ?? null

  return (
    <div className="h-dvh flex items-center justify-center bg-surface-primary px-4 py-8">
      <div className="w-full max-w-2xl space-y-6">
        {/* Language switcher */}
        <div className="flex justify-center">
          <div className="flex gap-1 rounded-lg border border-border p-0.5">
            {LANGUAGES.map((lang) => (
              <button
                key={lang.value}
                onClick={() => { setLocale(lang.value); i18n.changeLanguage(lang.value) }}
                className={[
                  'rounded-md px-2.5 py-1 text-xs font-medium transition-colors cursor-pointer',
                  locale === lang.value
                    ? 'bg-accent text-white'
                    : 'text-text-secondary hover:bg-surface-tertiary',
                ].join(' ')}
              >
                {lang.label}
              </button>
            ))}
          </div>
        </div>

        {/* Header */}
        <div className="text-center">
          <img src="/goclaw-icon.svg" alt="GoClaw" className="mx-auto mb-4 h-16 w-16" />
          <h1 className="text-4xl font-bold tracking-tight text-text-primary">{t('onboarding.welcome')}</h1>
          <p className="mt-2 text-sm text-text-muted">
            {t('onboarding.welcomeDesc')}
          </p>
        </div>

        <SetupStepper currentStep={step} completedSteps={completedSteps} />

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
    </div>
  )
}

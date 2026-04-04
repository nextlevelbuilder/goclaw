import { useTranslation } from 'react-i18next'

interface SetupStepperProps {
  currentStep: 1 | 2 | 3
  completedSteps: number[]
}

export function SetupStepper({ currentStep, completedSteps }: SetupStepperProps) {
  const { t } = useTranslation('desktop')

  const STEPS = [
    { num: 1, label: t('onboarding.stepProvider') },
    { num: 2, label: t('onboarding.stepModel') },
    { num: 3, label: t('onboarding.stepAgent') },
  ]

  return (
    <div className="flex items-center justify-center gap-0 mb-6">
      {STEPS.map((step, i) => {
        const isCompleted = completedSteps.includes(step.num)
        const isCurrent = step.num === currentStep

        return (
          <div key={step.num} className="flex items-center">
            <div className="flex flex-col items-center gap-1.5">
              <div
                className={[
                  'flex h-9 w-9 items-center justify-center rounded-full text-sm font-medium transition-colors',
                  isCompleted
                    ? 'bg-accent text-white'
                    : isCurrent
                      ? 'border-2 border-accent bg-surface-primary text-accent'
                      : 'border border-border bg-surface-tertiary text-text-muted',
                ].join(' ')}
              >
                {isCompleted ? (
                  <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                    <path d="M3 8L6.5 11.5L13 5" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
                  </svg>
                ) : step.num}
              </div>
              <span
                className={[
                  'text-xs font-medium',
                  isCurrent || isCompleted ? 'text-text-primary' : 'text-text-muted',
                ].join(' ')}
              >
                {step.label}
              </span>
            </div>

            {i < STEPS.length - 1 && (
              <div
                className={[
                  'mx-3 mb-6 h-0.5 w-12',
                  isCompleted ? 'bg-accent' : 'bg-border',
                ].join(' ')}
              />
            )}
          </div>
        )
      })}
    </div>
  )
}

import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'
import { IconCheck } from '../common/Icons'

interface SetupStepperProps {
  currentStep: 1 | 2 | 3
  completedSteps: number[]
}

const STEPS = [
  { num: 1, key: 'provider' },
  { num: 2, key: 'model' },
  { num: 3, key: 'agent' },
] as const

export function SetupStepper({ currentStep, completedSteps }: SetupStepperProps) {
  const { t } = useTranslation('onboarding')

  return (
    <ol aria-label={t('steps.label')} className="flex items-center gap-3 text-caption font-medium">
      {STEPS.map((step, i) => {
        const done = completedSteps.includes(step.num)
        const current = step.num === currentStep
        return (
          <li key={step.num} aria-current={current ? 'step' : undefined} className="flex items-center gap-3">
            <span className={cn('flex items-center gap-2', done ? 'text-success' : current ? 'text-ink' : 'text-ink-muted')}>
              <span
                className={cn(
                  'flex size-[22px] items-center justify-center rounded-pill border border-border-control',
                  done && 'border-transparent bg-success-subtle',
                  current && 'border-transparent bg-accent text-on-accent',
                )}
              >
                {done ? <IconCheck size={12} /> : step.num}
              </span>
              {t(`steps.${step.key}`)}
            </span>
            {i < STEPS.length - 1 && <span aria-hidden="true" className="h-px w-8 bg-border-control" />}
          </li>
        )
      })}
    </ol>
  )
}

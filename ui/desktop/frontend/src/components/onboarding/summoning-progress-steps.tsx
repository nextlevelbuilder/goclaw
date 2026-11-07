import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'
import { IconCheck } from '../common/Icons'

const SUMMONING_FILES = [
  { name: 'SOUL.md', labelKey: 'summoning.fileSOUL' },
  { name: 'IDENTITY.md', labelKey: 'summoning.fileIDENTITY' },
]

interface SummoningProgressStepsProps {
  generatedFiles: string[]
}

export function SummoningProgressSteps({ generatedFiles }: SummoningProgressStepsProps) {
  const { t } = useTranslation('onboarding')

  return (
    <ul aria-label={t('summoning.filesLabel')} className="w-full space-y-1">
      {SUMMONING_FILES.map((file, i) => {
        const done = generatedFiles.includes(file.name)
        return (
          <li key={file.name} className="flex items-center gap-3 rounded-md px-3 py-1.5">
            <span
              className={cn(
                'flex size-5 items-center justify-center rounded-pill text-caption',
                done ? 'bg-success-subtle text-success' : 'bg-surface-hover text-ink-muted',
              )}
            >
              {done ? <IconCheck size={12} /> : i + 1}
            </span>
            <span className="min-w-0 flex-1">
              <span className={cn('text-ui', done ? 'font-medium text-ink' : 'text-ink-secondary')}>{file.name}</span>
              <span className="ml-2 text-caption text-ink-muted">{t(file.labelKey)}</span>
            </span>
            {done && <span className="text-caption text-success">{t('summoning.done')}</span>}
          </li>
        )
      })}
    </ul>
  )
}

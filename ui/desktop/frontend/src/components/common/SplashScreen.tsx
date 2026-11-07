import { useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { GATEWAY_TIMEOUT_SECS, type BootPhase } from '../../hooks/use-gateway-boot'
import { wails } from '../../lib/wails'
import { cn } from '../../lib/cn'
import { IconAlertTriangle, IconCheck, IconDot, IconRefresh } from '../common/Icons'
import { Button, Spinner } from '../ui'

const PHASES: BootPhase[] = ['gateway', 'connect', 'setup']

function AppMark({ tone = 'accent' }: { tone?: 'accent' | 'danger' }) {
  return (
    <span
      aria-hidden="true"
      className={cn(
        'flex size-14 items-center justify-center rounded-xl text-display font-bold',
        tone === 'accent' ? 'bg-accent text-on-accent' : 'bg-danger-subtle text-danger',
      )}
    >
      G
    </span>
  )
}

/** Starting card: what the app is doing now, step by step. Shown while the gateway boots. */
export function SplashScreen({ phase }: { phase: BootPhase }) {
  const { t } = useTranslation('onboarding')
  const current = PHASES.indexOf(phase)

  return (
    <div role="status" aria-live="polite" className="flex h-dvh select-none flex-col items-center justify-center gap-6 bg-surface-window pt-titlebar animate-fade-in">
      <AppMark />
      <div className="text-center">
        <h1 className="text-title-lg text-ink">{t('start.title')}</h1>
        <p className="mt-1 text-caption text-ink-muted">{t('start.subtitle')}</p>
      </div>
      <ol className="flex flex-col gap-2.5 text-ui">
        {PHASES.map((p, i) => {
          const done = i < current
          const now = i === current
          return (
            <li key={p} className={cn('flex items-center gap-2.5', done ? 'text-ink-secondary' : now ? 'text-ink' : 'text-ink-muted')}>
              {done ? <IconCheck size={16} className="text-success" /> : now ? <Spinner size={14} className="text-accent" /> : <IconDot size={16} />}
              {t(`start.steps.${p}`)}
            </li>
          )
        })}
      </ol>
    </div>
  )
}

/** Start failed card: what happened, what to do, Try again and where the data lives. */
export function StartFailedScreen({ reason, detail, onRetry }: { reason: 'timeout' | 'connect'; detail?: string; onRetry: () => void }) {
  const { t } = useTranslation('onboarding')
  const [dataDir, setDataDir] = useState('')

  useEffect(() => {
    wails.getDataDir().then(setDataDir).catch(() => {})
  }, [])

  return (
    <div role="alert" className="flex h-dvh flex-col items-center justify-center gap-5 bg-surface-window px-6 pt-titlebar text-center">
      <span className="flex size-14 items-center justify-center rounded-xl bg-danger-subtle text-danger">
        <IconAlertTriangle size={24} />
      </span>
      <div className="flex max-w-md flex-col gap-1.5">
        <h1 className="text-title-lg text-ink">{t('start.failed.title')}</h1>
        <p className="text-body text-ink-secondary">
          {reason === 'timeout' ? t('start.failed.timeoutBody', { seconds: GATEWAY_TIMEOUT_SECS }) : t('start.failed.connectBody')}
        </p>
        <p className="text-caption text-ink-muted">{t('start.failed.todo')}</p>
      </div>
      {detail && (
        <p className="max-w-md rounded-md border border-border-subtle bg-surface-raised px-3 py-2 text-left text-code text-ink-secondary">
          <span className="text-ink-muted">{t('start.failed.detail')}: </span>{detail}
        </p>
      )}
      <Button variant="primary" icon={<IconRefresh size={14} />} onClick={onRetry}>{t('start.failed.tryAgain')}</Button>
      {dataDir && (
        <p className="text-caption text-ink-muted">
          {t('start.failed.dataDir')} <span className="text-code text-ink-secondary">{dataDir}</span>
        </p>
      )}
    </div>
  )
}

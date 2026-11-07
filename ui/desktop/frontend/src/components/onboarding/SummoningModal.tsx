import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { getWsClient } from '../../lib/ws'
import { agentService } from '../../services/agent-service'
import { IconAlertTriangle, IconCheckCircle } from '../common/Icons'
import { Button, Dialog, DialogFooter, Spinner } from '../ui'
import { SummoningProgressSteps } from './summoning-progress-steps'

const SUMMONING_REQUIRED_FILES = ['SOUL.md', 'IDENTITY.md']

interface SummoningModalProps {
  agentId: string
  agentName: string
  onContinue: () => void
  onCancel?: (agentId: string) => Promise<void>
}

const CANCEL_THRESHOLD_SEC = 60

export function SummoningModal({ agentId, agentName, onContinue, onCancel }: SummoningModalProps) {
  const { t } = useTranslation('onboarding')
  const [generatedFiles, setGeneratedFiles] = useState<string[]>([])
  const [status, setStatus] = useState<'summoning' | 'completed' | 'failed'>('summoning')
  const [errorMsg, setErrorMsg] = useState('')
  const [retrying, setRetrying] = useState(false)
  const [cancelling, setCancelling] = useState(false)
  const [elapsed, setElapsed] = useState(0)

  // Elapsed timer — runs only while summoning
  useEffect(() => {
    if (status !== 'summoning') return
    const id = setInterval(() => setElapsed((prev) => prev + 1), 1000)
    return () => clearInterval(id)
  }, [status])

  // Listen to agent.summoning WS events
  const handleSummoningEvent = useCallback(
    (payload: unknown) => {
      const data = payload as Record<string, string>
      if (data.agent_id !== agentId) return

      if (data.type === 'file_generated' && data.file) {
        setGeneratedFiles((prev) => (prev.includes(data.file) ? prev : [...prev, data.file]))
      }
      if (data.type === 'completed') {
        setGeneratedFiles((prev) => [...new Set([...prev, ...SUMMONING_REQUIRED_FILES])])
        setStatus('completed')
      }
      if (data.type === 'failed') {
        setStatus('failed')
        setErrorMsg(data.error || t('summoning.failed'))
      }
    },
    [agentId, t],
  )

  useEffect(() => {
    try {
      return getWsClient().on('agent.summoning', handleSummoningEvent)
    } catch { /* ws not ready */ }
  }, [handleSummoningEvent])

  const handleRetry = async () => {
    setRetrying(true)
    try {
      await agentService.resummonWs(agentId)
      setGeneratedFiles([])
      setStatus('summoning')
      setErrorMsg('')
      setElapsed(0)
    } catch {
      // stay in failed state
    } finally {
      setRetrying(false)
    }
  }

  const handleCancel = async () => {
    if (!onCancel) return
    setCancelling(true)
    try {
      await onCancel(agentId)
      // BE emits WS type=failed → listener above switches to failed state
    } catch {
      // keep dialog open
    } finally {
      setCancelling(false)
    }
  }

  const title = status === 'completed' ? t('summoning.completedTitle') : status === 'failed' ? t('summoning.failedTitle') : t('summoning.title')
  const time = `${Math.floor(elapsed / 60)}:${String(elapsed % 60).padStart(2, '0')}`

  return (
    <Dialog
      open
      onOpenChange={() => {}}
      dismissible={false}
      hideClose
      size="sm"
      title={title}
      footer={(
        <DialogFooter>
          {status === 'summoning' && elapsed >= CANCEL_THRESHOLD_SEC && onCancel && (
            <>
              <span className="mr-auto self-center text-caption text-ink-muted">{t('summoning.takingTooLong')}</span>
              <Button onClick={() => { void handleCancel() }} loading={cancelling}>
                {cancelling ? t('summoning.cancelling') : t('summoning.cancel')}
              </Button>
            </>
          )}
          {status === 'completed' && <Button variant="primary" onClick={onContinue}>{t('summoning.continue')}</Button>}
          {status === 'failed' && (
            <Button variant="primary" onClick={() => { void handleRetry() }} loading={retrying}>
              {retrying ? t('summoning.retrying') : t('summoning.retry')}
            </Button>
          )}
        </DialogFooter>
      )}
    >
      <div className="flex flex-col items-center gap-4">
        <div role="status" className="flex items-center gap-2 text-ui text-ink">
          {status === 'completed' && <IconCheckCircle size={18} className="text-success" />}
          {status === 'failed' && <IconAlertTriangle size={18} className="text-danger" />}
          {status === 'summoning' && <Spinner size={16} className="text-accent" />}
          <span className={status === 'failed' ? 'text-danger' : undefined}>
            {status === 'completed'
              ? t('summoning.ready', { name: agentName })
              : status === 'failed'
                ? errorMsg || t('summoning.failed')
                : t('summoning.weaving', { name: agentName })}
          </span>
        </div>
        <SummoningProgressSteps generatedFiles={generatedFiles} />
        {status === 'summoning' && <p className="text-caption tabular-nums text-ink-muted">{t('summoning.waiting', { time })}</p>}
      </div>
    </Dialog>
  )
}

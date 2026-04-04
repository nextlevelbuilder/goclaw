import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { motion } from 'framer-motion'
import { getWsClient } from '../../lib/ws'
import { agentService } from '../../services/agent-service'
import { SummoningProgressSteps } from './summoning-progress-steps'

const SUMMONING_REQUIRED_FILES = ['SOUL.md', 'IDENTITY.md']

interface SummoningModalProps {
  agentId: string
  agentName: string
  onContinue: () => void
}

export function SummoningModal({ agentId, agentName, onContinue }: SummoningModalProps) {
  const { t } = useTranslation(['desktop', 'common'])
  const [generatedFiles, setGeneratedFiles] = useState<string[]>([])
  const [status, setStatus] = useState<'summoning' | 'completed' | 'failed'>('summoning')
  const [errorMsg, setErrorMsg] = useState('')
  const [retrying, setRetrying] = useState(false)
  const [elapsed, setElapsed] = useState(0)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  // Elapsed timer — runs while summoning, stops on completion/failure
  useEffect(() => {
    if (status === 'summoning') {
      timerRef.current = setInterval(() => setElapsed((prev) => prev + 1), 1000)
    } else if (timerRef.current) {
      clearInterval(timerRef.current)
      timerRef.current = null
    }
    return () => {
      if (timerRef.current) {
        clearInterval(timerRef.current)
        timerRef.current = null
      }
    }
  }, [status])

  // Listen to agent.summoning WS events
  const handleSummoningEvent = useCallback(
    (payload: unknown) => {
      const data = payload as Record<string, string>
      if (data.agent_id !== agentId) return

      if (data.type === 'file_generated' && data.file) {
        setGeneratedFiles((prev) =>
          prev.includes(data.file) ? prev : [...prev, data.file],
        )
      }
      if (data.type === 'completed') {
        setGeneratedFiles((prev) => [...new Set([...prev, ...SUMMONING_REQUIRED_FILES])])
        setStatus('completed')
      }
      if (data.type === 'failed') {
        setStatus('failed')
        setErrorMsg(data.error || t('desktop:summoning.failed'))
      }
    },
    [agentId],
  )

  useEffect(() => {
    try {
      const ws = getWsClient()
      const unsub = ws.on('agent.summoning', handleSummoningEvent)
      return unsub
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

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center bg-black/50 backdrop-blur-sm">
      <div className="bg-surface-secondary border border-border rounded-2xl shadow-xl max-w-md w-full mx-4">
        {/* Header */}
        <div className="pt-6 pb-2 text-center">
          <h2 className="text-lg font-semibold text-text-primary">
            {status === 'completed'
              ? t('desktop:summoning.completedTitle')
              : status === 'failed'
                ? t('desktop:summoning.failedTitle')
                : t('desktop:summoning.title')}
          </h2>
        </div>

        <div className="flex flex-col items-center gap-6 px-6 py-6">
          {/* Animated orb — ported from web UI */}
          <div className="relative flex h-24 w-24 items-center justify-center">
            {status === 'summoning' && (
              <>
                <motion.div
                  className="absolute inset-0 rounded-full bg-orange-500/20"
                  animate={{ scale: [1, 1.3, 1], opacity: [0.3, 0.1, 0.3] }}
                  transition={{ duration: 2, repeat: Infinity, ease: 'easeInOut' }}
                />
                <motion.div
                  className="absolute inset-2 rounded-full bg-orange-500/30"
                  animate={{ scale: [1, 1.15, 1], opacity: [0.5, 0.2, 0.5] }}
                  transition={{ duration: 1.5, repeat: Infinity, ease: 'easeInOut', delay: 0.3 }}
                />
              </>
            )}
            <motion.div
              className={`relative z-10 flex h-16 w-16 items-center justify-center rounded-full text-3xl ${
                status === 'completed'
                  ? 'bg-emerald-100 dark:bg-emerald-900/30'
                  : status === 'failed'
                    ? 'bg-red-100 dark:bg-red-900/30'
                    : 'bg-orange-100 dark:bg-orange-900/30'
              }`}
              animate={
                status === 'summoning'
                  ? { rotate: [0, 5, -5, 0] }
                  : status === 'completed'
                    ? { scale: [1, 1.2, 1] }
                    : {}
              }
              transition={
                status === 'summoning'
                  ? { duration: 3, repeat: Infinity, ease: 'easeInOut' }
                  : { duration: 0.5 }
              }
            >
              {status === 'completed' ? '✨' : status === 'failed' ? '💨' : '🪄'}
            </motion.div>
          </div>

          {/* Agent name */}
          <p className="text-sm text-text-primary">
            {status === 'completed' ? (
              <span className="font-medium text-emerald-600 dark:text-emerald-400">
                {agentName} is ready!
              </span>
            ) : status === 'failed' ? (
              <span className="font-medium text-red-600 dark:text-red-400">
                {errorMsg || t('desktop:summoning.failed')}
              </span>
            ) : (
              <>Weaving soul for <span className="font-semibold text-text-primary">{agentName}</span>...</>
            )}
          </p>

          {/* File progress */}
          <SummoningProgressSteps generatedFiles={generatedFiles} />

          {status === 'summoning' && (
            <p className="text-center text-xs text-text-muted tabular-nums">
              Please wait... ({Math.floor(elapsed / 60)}:{String(elapsed % 60).padStart(2, '0')})
            </p>
          )}

          {status === 'completed' && (
            <button
              onClick={onContinue}
              className="px-5 py-2 bg-accent text-white rounded-lg text-sm font-medium hover:bg-accent-hover transition-colors"
            >
              Continue →
            </button>
          )}

          {status === 'failed' && (
            <button
              onClick={handleRetry}
              disabled={retrying}
              className="px-4 py-2 border border-border rounded-lg text-sm text-text-secondary hover:bg-surface-tertiary transition-colors disabled:opacity-50"
            >
              {retrying ? t('desktop:summoning.retrying') : t('common:retry')}
            </button>
          )}
        </div>
      </div>
    </div>
  )
}

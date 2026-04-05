import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import type { CronJob, CronRunLog } from '../../../types/cron'

interface CronRunsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  job: CronJob
  onFetchRuns: (jobId: string) => Promise<CronRunLog[]>
}

function statusBadge(status: string) {
  if (status === 'ok' || status === 'success') {
    return 'bg-emerald-500/15 text-emerald-700 border border-emerald-500/25 dark:text-emerald-400 dark:bg-emerald-500/10 dark:border-emerald-500/20'
  }
  if (status === 'error' || status === 'failed') {
    return 'bg-red-500/15 text-red-700 border border-red-500/25 dark:text-red-400 dark:bg-red-500/10 dark:border-red-500/20'
  }
  if (status === 'running') {
    return 'bg-blue-500/15 text-blue-700 border border-blue-500/25 dark:text-blue-400 dark:bg-blue-500/10 dark:border-blue-500/20 animate-pulse'
  }
  return 'bg-surface-tertiary text-text-secondary border border-border'
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

export function CronRunsDialog({ open, onOpenChange, job, onFetchRuns }: CronRunsDialogProps) {
  const { t } = useTranslation('cron')
  const [runs, setRuns] = useState<CronRunLog[]>([])
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (!open) return
    setLoading(true)
    onFetchRuns(job.id)
      .then(setRuns)
      .catch(() => setRuns([]))
      .finally(() => setLoading(false))
  }, [open, job.id, onFetchRuns])

  if (!open) return null

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" onClick={() => onOpenChange(false)} />
      <div className="relative w-full max-w-lg bg-surface-secondary rounded-xl border border-border overflow-hidden">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-5 py-4">
          <span className="text-sm font-semibold text-text-primary">
            {t('runLog.title', { name: job.name })}
          </span>
          <button
            onClick={() => onOpenChange(false)}
            className="p-1 text-text-muted hover:text-text-primary transition-colors"
          >
            <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M18 6 6 18" /><path d="m6 6 12 12" />
            </svg>
          </button>
        </div>

        {/* Content */}
        <div className="max-h-[60vh] overflow-y-auto">
          {loading ? (
            <div className="flex items-center justify-center py-12">
              <svg className="h-5 w-5 animate-spin text-text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                <path d="M21 12a9 9 0 1 1-6.219-8.56" />
              </svg>
              <span className="ml-2 text-sm text-text-muted">{t('runLog.loading')}</span>
            </div>
          ) : runs.length === 0 ? (
            <div className="flex flex-col items-center gap-2 py-12">
              <p className="text-sm text-text-muted">{t('runLog.noHistory')}</p>
            </div>
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full min-w-[520px] text-sm">
                <thead>
                  <tr className="border-b border-border bg-surface-tertiary/40">
                    <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Time</th>
                    <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Status</th>
                    <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Duration</th>
                    <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Tokens</th>
                    <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">Summary</th>
                  </tr>
                </thead>
                <tbody>
                  {runs.map((run, idx) => (
                    <tr key={idx} className="border-b border-border last:border-0 hover:bg-surface-tertiary/30 transition-colors">
                      <td className="px-4 py-3 text-xs text-text-muted whitespace-nowrap">
                        {new Date(run.ts).toLocaleString()}
                      </td>
                      <td className="px-4 py-3">
                        <span className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${statusBadge(run.status)}`}>
                          {run.status}
                        </span>
                        {run.error && (
                          <p className="text-[10px] text-error mt-0.5 max-w-[120px] truncate" title={run.error}>
                            {run.error}
                          </p>
                        )}
                      </td>
                      <td className="px-4 py-3 text-xs text-text-muted">
                        {formatDuration(run.durationMs)}
                      </td>
                      <td className="px-4 py-3 text-xs text-text-muted">
                        {t('detail.inOut', { input: run.inputTokens, output: run.outputTokens })}
                      </td>
                      <td className="px-4 py-3 text-xs text-text-muted max-w-[160px] truncate" title={run.summary}>
                        {run.summary || '—'}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>

        {/* Footer */}
        <div className="border-t border-border px-5 py-3 flex justify-end">
          <button
            onClick={() => onOpenChange(false)}
            className="border border-border rounded-lg px-4 py-1.5 text-sm text-text-secondary hover:bg-surface-tertiary transition-colors"
          >
            Close
          </button>
        </div>
      </div>
    </div>
  )
}

import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../../lib/api'
import { IconClock } from '../common/Icons'
import { Button, DataTable, Dialog, DialogFooter, EmptyState, ErrorState, SkeletonList, StatusChip, type DataTableColumn } from '../ui'
import { formatRunDuration, runStatus } from './cron-format'
import type { CronJob, CronRunLog } from '../../types/cron'

interface CronRunsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  job: CronJob
  onFetchRuns: (jobId: string) => Promise<CronRunLog[]>
}

export function CronRunsDialog({ open, onOpenChange, job, onFetchRuns }: CronRunsDialogProps) {
  const { t, i18n } = useTranslation(['cron', 'common'])
  const [runs, setRuns] = useState<CronRunLog[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<Error | null>(null)

  const load = useCallback(() => {
    setLoading(true)
    setError(null)
    onFetchRuns(job.id)
      .then(setRuns)
      .catch((err) => setError(err instanceof Error ? err : new Error(String(err))))
      .finally(() => setLoading(false))
  }, [job.id, onFetchRuns])

  useEffect(() => {
    if (open) load()
  }, [open, load])

  const columns: DataTableColumn<CronRunLog>[] = [
    {
      id: 'time',
      header: t('cron:runLog.time'),
      className: 'whitespace-nowrap',
      cell: (run) => <span className="text-ink-secondary">{new Date(run.ts).toLocaleString(i18n.language)}</span>,
    },
    {
      id: 'status',
      header: t('cron:runLog.status'),
      cell: (run) => {
        const status = runStatus(run.status)
        return (
          <div className="min-w-0">
            {status ? <StatusChip tone={status.tone} label={t(`cron:${status.key}`)} /> : <span className="text-ink-muted">{run.status}</span>}
            {run.error && <p role="note" className="mt-0.5 max-w-48 truncate text-caption text-danger" title={run.error}>{run.error}</p>}
          </div>
        )
      },
    },
    {
      id: 'duration',
      header: t('cron:runLog.duration'),
      cell: (run) => <span className="text-ink-secondary">{formatRunDuration(run.durationMs)}</span>,
    },
    {
      id: 'tokens',
      header: t('cron:runLog.tokens'),
      cell: (run) => <span className="text-ink-secondary">{t('cron:detail.inOut', { input: run.inputTokens, output: run.outputTokens })}</span>,
    },
    {
      id: 'summary',
      header: t('cron:runLog.summary'),
      cell: (run) => <span className="block max-w-48 truncate text-ink-secondary" title={run.summary}>{run.summary || '—'}</span>,
    },
  ]

  let body: React.ReactNode
  if (loading) {
    body = <SkeletonList rows={4} avatar={false} label={t('cron:runLog.loading')} />
  } else if (error) {
    body = (
      <ErrorState
        title={t('cron:runLog.errorTitle')}
        description={t('cron:runLog.errorBody')}
        code={error instanceof ApiError ? String(error.status) : undefined}
        onRetry={load}
      />
    )
  } else if (runs.length === 0) {
    body = <EmptyState icon={<IconClock size={20} />} title={t('cron:runLog.noHistory')} description={t('cron:runLog.noHistoryDescription')} />
  } else {
    body = <DataTable aria-label={t('cron:runLog.title', { name: job.name })} columns={columns} rows={runs} getRowId={(run) => `${run.ts}`} />
  }

  return (
    <Dialog
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title={t('cron:runLog.title', { name: job.name })}
      footer={(
        <DialogFooter>
          <Button variant="secondary" onClick={() => onOpenChange(false)}>{t('common:close')}</Button>
        </DialogFooter>
      )}
    >
      <div className="max-h-[60vh] overflow-y-auto overscroll-contain">{body}</div>
    </Dialog>
  )
}

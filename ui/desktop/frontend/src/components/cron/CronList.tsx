import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useCron } from '../../hooks/use-cron'
import { useAgentCrud } from '../../hooks/use-agent-crud'
import { useGatewayStatus } from '../layout/use-gateway-status'
import { ApiError } from '../../lib/api'
import { cn } from '../../lib/cn'
import { IconClock, IconGear, IconPlus, IconRefresh, IconTrash } from '../common/Icons'
import {
  Button, ConfirmDialog, DataTable, EmptyState, ErrorState, Menu, PageHeader, SkeletonList, StatusChip, Switch,
  type DataTableColumn,
} from '../ui'
import { CronFormDialog } from './CronFormDialog'
import { CronRunsDialog } from './CronRunsDialog'
import { formatSchedule, formatWhen, runStatus } from './cron-format'
import type { CronJob } from '../../types/cron'

export function CronList() {
  const { t, i18n } = useTranslation(['cron', 'common'])
  const { jobs, loading, error, fetchJobs, createJob, updateJob, deleteJob, toggleJob, runJob, fetchRuns } = useCron()
  const { agents } = useAgentCrud()
  const { disconnected } = useGatewayStatus()

  const [formJob, setFormJob] = useState<CronJob | 'new' | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<CronJob | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [runsTarget, setRunsTarget] = useState<CronJob | null>(null)
  const [runningIds, setRunningIds] = useState<Set<string>>(new Set())

  function agentName(agentId: string): string {
    if (!agentId) return t('cron:defaultAgent')
    const agent = agents.find((a) => a.id === agentId)
    return agent?.display_name || agent?.agent_key || agentId
  }

  async function handleRunNow(job: CronJob) {
    setRunningIds((prev) => new Set(prev).add(job.id))
    try {
      await runJob(job.id)
    } catch {
      // The hook already toasted the failure.
    } finally {
      setRunningIds((prev) => { const s = new Set(prev); s.delete(job.id); return s })
    }
  }

  async function handleDeleteConfirm() {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await deleteJob(deleteTarget.id)
      setDeleteTarget(null)
    } catch {
      // Keep the dialog open so the user can retry.
    } finally {
      setDeleting(false)
    }
  }

  const columns: DataTableColumn<CronJob>[] = [
    {
      id: 'name',
      header: t('cron:columns.name'),
      cell: (job) => (
        <div className="min-w-0">
          <p className="truncate text-code text-ink">{job.name}</p>
          <p className="truncate text-caption text-ink-muted">{agentName(job.agentId)}</p>
        </div>
      ),
    },
    {
      id: 'schedule',
      header: t('cron:columns.schedule'),
      cell: (job) => <span className="text-code text-ink-secondary">{formatSchedule(t, i18n.language, job.schedule)}</span>,
    },
    {
      id: 'next',
      header: t('cron:columns.nextRun'),
      className: 'whitespace-nowrap',
      cell: (job) => (
        <span className="text-ink-secondary">
          {!job.enabled ? t('cron:paused') : formatWhen(job.state?.nextRunAtMs, i18n.language)}
        </span>
      ),
    },
    {
      id: 'last',
      header: t('cron:columns.lastRun'),
      className: 'w-40',
      cell: (job) => {
        if (!job.enabled) return <StatusChip tone="idle" label={t('cron:status.paused')} />
        const status = runStatus(job.state?.lastStatus)
        if (!status) return <span className="text-ink-muted">—</span>
        return <StatusChip tone={status.tone} label={t(`cron:${status.key}`)} title={job.state?.lastError} />
      },
    },
    {
      id: 'enabled',
      header: t('cron:columns.enabled'),
      className: 'w-24',
      cell: (job) => (
        <span onClick={(e) => e.stopPropagation()} className="inline-flex">
          <Switch
            checked={job.enabled}
            disabled={disconnected}
            aria-label={t('cron:enableToggle', { name: job.name })}
            onCheckedChange={() => { void toggleJob(job.id).catch(() => {}) }}
          />
        </span>
      ),
    },
    {
      id: 'actions',
      header: <span className="sr-only">{t('cron:columns.actions')}</span>,
      className: 'w-12 text-right',
      cell: (job) => (
        <span onClick={(e) => e.stopPropagation()} className="inline-flex">
          <Menu
            label={t('cron:rowMenu', { name: job.name })}
            items={[
              { label: runningIds.has(job.id) ? t('cron:running') : t('cron:runNow'), icon: <IconRefresh size={14} />, onSelect: () => { void handleRunNow(job) }, disabled: disconnected || runningIds.has(job.id) },
              { label: t('cron:runHistory'), icon: <IconClock size={14} />, onSelect: () => setRunsTarget(job) },
              { label: t('cron:edit.menu'), icon: <IconGear size={14} />, onSelect: () => setFormJob(job), disabled: disconnected },
              'separator',
              { label: t('cron:delete.confirmLabel'), icon: <IconTrash size={14} />, destructive: true, onSelect: () => setDeleteTarget(job), disabled: disconnected },
            ]}
          />
        </span>
      ),
    },
  ]

  let body: React.ReactNode
  if (loading) {
    body = <SkeletonList rows={4} avatar={false} label={t('common:state.loading')} className="rounded-lg border border-border-subtle" />
  } else if (error && jobs.length === 0) {
    body = (
      <ErrorState
        title={t('cron:states.errorTitle')}
        description={t('cron:states.errorBody')}
        code={error instanceof ApiError ? String(error.status) : undefined}
        onRetry={() => { void fetchJobs() }}
      />
    )
  } else if (jobs.length === 0) {
    body = (
      <EmptyState
        icon={<IconClock size={20} />}
        title={t('cron:emptyTitle')}
        description={t('cron:emptyDescription')}
        action={<Button variant="primary" icon={<IconPlus size={16} />} onClick={() => setFormJob('new')} disabled={disconnected}>{t('cron:newJob')}</Button>}
      />
    )
  } else {
    body = (
      <DataTable
        aria-label={t('cron:title')}
        columns={columns}
        rows={jobs}
        getRowId={(job) => job.id}
        onRowClick={(job) => setRunsTarget(job)}
      />
    )
  }

  const activeCount = jobs.filter((j) => j.enabled).length
  const editing = formJob && formJob !== 'new' ? formJob : null

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader
        title={t('cron:title')}
        description={jobs.length > 0 ? t('cron:summary', { count: jobs.length, active: activeCount }) : t('cron:description')}
        actions={jobs.length > 0 && (
          <Button variant="primary" icon={<IconPlus size={16} />} onClick={() => setFormJob('new')} disabled={disconnected}>
            {t('cron:newJob')}
          </Button>
        )}
      />
      <div className={cn('min-h-0 flex-1 overflow-y-auto overscroll-contain px-6 py-4', disconnected && 'opacity-60')}>
        {body}
      </div>

      <CronFormDialog
        open={formJob !== null}
        onOpenChange={(open) => { if (!open) setFormJob(null) }}
        job={editing}
        onCreate={async (params) => { await createJob(params) }}
        onUpdate={async (id, patch) => { await updateJob(id, patch) }}
        onDelete={(job) => { setFormJob(null); setDeleteTarget(job) }}
        onViewLog={(job) => { setFormJob(null); setRunsTarget(job) }}
      />

      <ConfirmDialog
        open={!!deleteTarget}
        onOpenChange={(open) => { if (!open) setDeleteTarget(null) }}
        title={t('cron:delete.title', { name: deleteTarget?.name ?? '' })}
        description={t('cron:delete.description', { name: deleteTarget?.name ?? '' })}
        confirmLabel={t('cron:delete.confirmLabel')}
        variant="destructive"
        loading={deleting}
        onConfirm={handleDeleteConfirm}
      />

      {runsTarget && (
        <CronRunsDialog open onOpenChange={() => setRunsTarget(null)} job={runsTarget} onFetchRuns={fetchRuns} />
      )}
    </div>
  )
}

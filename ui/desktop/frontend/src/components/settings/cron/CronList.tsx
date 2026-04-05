import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useCron } from '../../../hooks/use-cron'
import { useAgentCrud } from '../../../hooks/use-agent-crud'
import { RefreshButton } from '../../common/RefreshButton'
import { Switch } from '../../common/Switch'
import { ConfirmDialog } from '../../common/ConfirmDialog'
import { CronFormDialog } from './CronFormDialog'
import { CronRunsDialog } from './CronRunsDialog'
import type { CronJob, CronSchedule } from '../../../types/cron'

function formatSchedule(s: CronSchedule): string {
  if (s.kind === 'every' && s.everyMs) {
    const sec = s.everyMs / 1000
    if (sec < 60) return `every ${sec}s`
    if (sec < 3600) return `every ${Math.round(sec / 60)}m`
    return `every ${Math.round(sec / 3600)}h`
  }
  if (s.kind === 'cron' && s.expr) return s.expr
  if (s.kind === 'at' && s.atMs) return `once at ${new Date(s.atMs).toLocaleString()}`
  return '—'
}

function statusBadgeClass(status?: string): string {
  if (!status) return 'bg-surface-tertiary text-text-secondary border border-border'
  if (status === 'ok' || status === 'success') {
    return 'bg-emerald-500/15 text-emerald-700 border border-emerald-500/25 dark:text-emerald-400 dark:bg-emerald-500/10 dark:border-emerald-500/20'
  }
  if (status === 'error' || status === 'failed') {
    return 'bg-red-500/15 text-red-700 border border-red-500/25 dark:text-red-400 dark:bg-red-500/10 dark:border-red-500/20'
  }
  if (status === 'running') {
    return 'bg-blue-500/15 text-blue-700 border border-blue-500/25 dark:text-blue-400 dark:bg-blue-500/10 dark:border-blue-500/20 animate-pulse'
  }
  return 'bg-amber-500/15 text-amber-700 border border-amber-500/25 dark:text-amber-400 dark:bg-amber-500/10 dark:border-amber-500/20'
}

export function CronList() {
  const { t } = useTranslation('cron')
  const { jobs, loading, fetchJobs, createJob, deleteJob, toggleJob, runJob, fetchRuns } = useCron()
  const { agents } = useAgentCrud()

  const [formOpen, setFormOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<CronJob | null>(null)
  const [toggleTarget, setToggleTarget] = useState<CronJob | null>(null)
  const [runsTarget, setRunsTarget] = useState<CronJob | null>(null)
  const [runningIds, setRunningIds] = useState<Set<string>>(new Set())

  function agentName(agentId: string): string {
    if (!agentId) return t('defaultAgent')
    return agents.find((a) => a.id === agentId)?.display_name
      || agents.find((a) => a.id === agentId)?.agent_key
      || agentId
  }

  async function handleRunNow(job: CronJob) {
    setRunningIds((prev) => new Set(prev).add(job.id))
    try {
      await runJob(job.id)
    } finally {
      setRunningIds((prev) => { const s = new Set(prev); s.delete(job.id); return s })
    }
  }

  async function handleToggleConfirm() {
    if (!toggleTarget) return
    await toggleJob(toggleTarget.id)
    setToggleTarget(null)
  }

  async function handleDeleteConfirm() {
    if (!deleteTarget) return
    await deleteJob(deleteTarget.id)
    setDeleteTarget(null)
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">{t('title')}</h2>
          <p className="text-xs text-text-muted mt-0.5">{t('description')}</p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setFormOpen(true)}
            className="bg-accent text-white rounded-lg px-3 py-1.5 text-xs hover:bg-accent-hover transition-colors flex items-center gap-1.5"
          >
            <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M5 12h14" /><path d="M12 5v14" />
            </svg>
            {t('newJob')}
          </button>
          <RefreshButton onRefresh={fetchJobs} />
        </div>
      </div>

      {/* Loading skeleton */}
      {loading ? (
        <div className="space-y-2">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-12 rounded-lg bg-surface-tertiary/50 animate-pulse" />
          ))}
        </div>
      ) : jobs.length === 0 ? (
        /* Empty state */
        <div className="flex flex-col items-center gap-2 py-12">
          <svg className="h-10 w-10 text-text-muted/40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
            <circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" />
          </svg>
          <p className="text-sm text-text-muted">{t('emptyTitle')}</p>
          <p className="text-xs text-text-muted/70">{t('emptyDescription')}</p>
        </div>
      ) : (
        /* Table */
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="w-full min-w-[600px] text-sm">
            <thead>
              <tr className="border-b border-border bg-surface-tertiary/40">
                <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.name')}</th>
                <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.schedule')}</th>
                <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.agent')}</th>
                <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.enabled')}</th>
                <th className="px-4 py-2.5 text-right text-xs font-medium text-text-muted">{t('columns.actions')}</th>
              </tr>
            </thead>
            <tbody>
              {jobs.map((job) => (
                <tr key={job.id} className="border-b border-border last:border-0 hover:bg-surface-tertiary/30 transition-colors [&>td]:align-middle">
                  {/* Name */}
                  <td className="px-4 py-3">
                    <div className="flex items-center gap-2">
                      <svg className="h-4 w-4 text-text-muted shrink-0" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                        <circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" />
                      </svg>
                      <div className="min-w-0">
                        <span className="font-mono text-sm text-text-primary">{job.name}</span>
                        {job.state?.lastStatus && (
                          <span className={`ml-2 rounded-full px-1.5 py-0.5 text-[10px] font-medium ${statusBadgeClass(job.state.lastStatus)}`}>
                            {job.state.lastStatus}
                          </span>
                        )}
                      </div>
                    </div>
                  </td>
                  {/* Schedule */}
                  <td className="px-4 py-3 text-xs text-text-muted font-mono">
                    {formatSchedule(job.schedule)}
                  </td>
                  {/* Agent */}
                  <td className="px-4 py-3 text-xs text-text-muted">
                    {agentName(job.agentId)}
                  </td>
                  {/* Enabled badge */}
                  <td className="px-4 py-3">
                    <span className={`rounded-full px-2 py-0.5 text-[11px] font-medium ${
                      job.enabled
                        ? 'bg-emerald-500/15 text-emerald-700 border border-emerald-500/25 dark:text-emerald-400 dark:bg-emerald-500/10 dark:border-emerald-500/20'
                        : 'bg-surface-tertiary text-text-secondary border border-border'
                    }`}>
                      {job.enabled ? t('detail.enabled') : t('detail.disabled')}
                    </span>
                  </td>
                  {/* Actions */}
                  <td className="px-4 py-3 text-right">
                    <div className="flex items-center justify-end gap-1">
                      {/* Run history */}
                      <button
                        onClick={() => setRunsTarget(job)}
                        className="p-1 text-text-muted hover:text-text-primary transition-colors"
                        title={t('runHistory')}
                      >
                        <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                          <path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" />
                          <path d="M3 3v5h5" /><path d="M12 7v5l4 2" />
                        </svg>
                      </button>
                      {/* Run now */}
                      <button
                        onClick={() => handleRunNow(job)}
                        disabled={runningIds.has(job.id)}
                        className="p-1 text-text-muted hover:text-accent transition-colors disabled:opacity-50"
                        title={t('runNow')}
                      >
                        {runningIds.has(job.id) ? (
                          <svg className="h-3.5 w-3.5 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                            <path d="M21 12a9 9 0 1 1-6.219-8.56" />
                          </svg>
                        ) : (
                          <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                            <polygon points="6 3 20 12 6 21 6 3" />
                          </svg>
                        )}
                      </button>
                      {/* Toggle */}
                      <Switch
                        checked={job.enabled}
                        onCheckedChange={() => setToggleTarget(job)}
                      />
                      {/* Delete */}
                      <button
                        onClick={() => setDeleteTarget(job)}
                        className="p-1 text-text-muted hover:text-error transition-colors"
                        title={t('delete.title')}
                      >
                        <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                          <polyline points="3 6 5 6 21 6" /><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
                        </svg>
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Create dialog */}
      <CronFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        onSubmit={async (params) => { await createJob(params) }}
      />

      {/* Toggle confirm */}
      {toggleTarget && (
        <ConfirmDialog
          open
          onOpenChange={() => setToggleTarget(null)}
          title={toggleTarget.enabled ? t('disable.title') : t('enable.title')}
          description={toggleTarget.enabled
            ? t('disable.description', { name: toggleTarget.name })
            : t('enable.description', { name: toggleTarget.name })
          }
          confirmLabel={toggleTarget.enabled ? t('disable.confirmLabel') : t('enable.confirmLabel')}
          onConfirm={handleToggleConfirm}
        />
      )}

      {/* Delete confirm */}
      {deleteTarget && (
        <ConfirmDialog
          open
          onOpenChange={() => setDeleteTarget(null)}
          title={t('delete.title')}
          description={t('delete.description', { name: deleteTarget.name })}
          confirmLabel={t('delete.confirmLabel')}
          variant="destructive"
          onConfirm={handleDeleteConfirm}
        />
      )}

      {/* Run history */}
      {runsTarget && (
        <CronRunsDialog
          open
          onOpenChange={() => setRunsTarget(null)}
          job={runsTarget}
          onFetchRuns={fetchRuns}
        />
      )}
    </div>
  )
}

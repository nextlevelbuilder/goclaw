import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useCron } from '../../hooks/use-cron'
import { useAgentCrud } from '../../hooks/use-agent-crud'
import { RefreshButton } from '../common/RefreshButton'
import { ConfirmDialog } from '../common/ConfirmDialog'
import { CronFormDialog } from './CronFormDialog'
import { CronRunsDialog } from './CronRunsDialog'
import { CronListItem } from './cron-list-item'
import type { CronJob } from '../../types/cron'

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
        <div className="flex flex-col items-center gap-2 py-12">
          <svg className="h-10 w-10 text-text-muted/40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
            <circle cx="12" cy="12" r="10" /><polyline points="12 6 12 12 16 14" />
          </svg>
          <p className="text-sm text-text-muted">{t('emptyTitle')}</p>
          <p className="text-xs text-text-muted/70">{t('emptyDescription')}</p>
        </div>
      ) : (
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
                <CronListItem
                  key={job.id}
                  job={job}
                  agentName={agentName}
                  isRunning={runningIds.has(job.id)}
                  onRunNow={handleRunNow}
                  onToggle={(j) => setToggleTarget(j)}
                  onDelete={(j) => setDeleteTarget(j)}
                  onViewHistory={(j) => setRunsTarget(j)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      <CronFormDialog
        open={formOpen}
        onOpenChange={setFormOpen}
        onSubmit={async (params) => { await createJob(params) }}
      />

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

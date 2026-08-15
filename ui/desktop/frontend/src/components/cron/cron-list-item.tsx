// Individual cron job table row with actions: run history, run now, toggle, delete.

import { useTranslation } from 'react-i18next'
import { Switch } from '../common/Switch'
import type { CronJob, CronSchedule } from '../../types/cron'

export function formatSchedule(s: CronSchedule): string {
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

export function statusBadgeClass(status?: string): string {
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

interface CronListItemProps {
  job: CronJob
  agentName: (agentId: string) => string
  isRunning: boolean
  onRunNow: (job: CronJob) => void
  onToggle: (job: CronJob) => void
  onDelete: (job: CronJob) => void
  onViewHistory: (job: CronJob) => void
}

export function CronListItem({
  job, agentName, isRunning, onRunNow, onToggle, onDelete, onViewHistory,
}: CronListItemProps) {
  const { t } = useTranslation('cron')

  return (
    <tr className="border-b border-border last:border-0 hover:bg-surface-tertiary/30 transition-colors [&>td]:align-middle">
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
          <button
            onClick={() => onViewHistory(job)}
            className="p-1 text-text-muted hover:text-text-primary transition-colors"
            title={t('runHistory')}
          >
            <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
              <path d="M3 12a9 9 0 1 0 9-9 9.75 9.75 0 0 0-6.74 2.74L3 8" />
              <path d="M3 3v5h5" /><path d="M12 7v5l4 2" />
            </svg>
          </button>
          <button
            onClick={() => onRunNow(job)}
            disabled={isRunning}
            className="p-1 text-text-muted hover:text-accent transition-colors disabled:opacity-50"
            title={t('runNow')}
          >
            {isRunning ? (
              <svg className="h-3.5 w-3.5 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                <path d="M21 12a9 9 0 1 1-6.219-8.56" />
              </svg>
            ) : (
              <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                <polygon points="6 3 20 12 6 21 6 3" />
              </svg>
            )}
          </button>
          <Switch checked={job.enabled} onCheckedChange={() => onToggle(job)} />
          <button
            onClick={() => onDelete(job)}
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
  )
}

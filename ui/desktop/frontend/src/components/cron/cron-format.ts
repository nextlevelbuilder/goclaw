// Formatting helpers shared by the cron list, form and run log.
import type { TFunction } from 'i18next'
import type { CronSchedule } from '../../types/cron'

/** Human-readable schedule ("Every 5 min", "0 2 * * *"); the raw expression stays visible for cron jobs. */
export function formatSchedule(t: TFunction, locale: string, s: CronSchedule): string {
  if (s.kind === 'every' && s.everyMs) {
    const sec = s.everyMs / 1000
    if (sec < 60) return t('cron:schedule.everySeconds', { count: Math.round(sec) })
    if (sec < 3600) return t('cron:schedule.everyMinutes', { count: Math.round(sec / 60) })
    return t('cron:schedule.everyHours', { count: Math.round(sec / 3600) })
  }
  if (s.kind === 'cron' && s.expr) return s.expr
  if (s.kind === 'at' && s.atMs) return t('cron:schedule.onceAt', { time: new Date(s.atMs).toLocaleString(locale) })
  return '—'
}

/** Short date and time for next/last run columns. */
export function formatWhen(ms: number | undefined, locale: string): string {
  if (!ms) return '—'
  return new Date(ms).toLocaleString(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

export function formatRunDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

export type RunTone = 'success' | 'danger' | 'info' | 'warning' | 'idle'

/** Maps a gateway run status to a chip tone and an i18n label key. */
export function runStatus(status: string | undefined): { tone: RunTone; key: string } | null {
  if (!status) return null
  const s = status.toLowerCase()
  if (s === 'ok' || s === 'success') return { tone: 'success', key: 'status.succeeded' }
  if (s === 'error' || s === 'failed') return { tone: 'danger', key: 'status.failed' }
  if (s === 'running') return { tone: 'info', key: 'status.running' }
  return { tone: 'warning', key: 'status.other' }
}

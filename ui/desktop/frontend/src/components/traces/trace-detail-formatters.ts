// Pure formatting utilities for trace detail display.

export function formatDuration(ms: number | undefined | null, startTime?: string, endTime?: string): string {
  // Fallback: compute from start/end times if ms is missing
  if (ms == null || isNaN(ms) || ms === 0) {
    if (startTime && endTime) {
      const computed = new Date(endTime).getTime() - new Date(startTime).getTime()
      if (!isNaN(computed) && computed > 0) ms = computed
      else return '—'
    } else {
      return '—'
    }
  }
  if (ms < 1000) return `${ms}ms`
  if (ms < 60000) return `${(ms / 1000).toFixed(1)}s`
  const min = Math.floor(ms / 60000)
  const sec = Math.round((ms % 60000) / 1000)
  return `${min}m ${sec}s`
}

export function formatTokens(count: number | null | undefined): string {
  if (count == null) return '0'
  if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`
  if (count >= 1_000) return `${(count / 1_000).toFixed(1)}K`
  return count.toString()
}

export type TraceTone = 'success' | 'warning' | 'danger' | 'info' | 'idle'

/** Maps a gateway trace or span status to a status chip tone. */
export function statusTone(status: string): TraceTone {
  const s = status.toLowerCase()
  if (s === 'completed' || s === 'ok' || s === 'success') return 'success'
  if (s === 'error' || s === 'failed') return 'danger'
  if (s === 'running') return 'info'
  return 'idle'
}

/** Known statuses have an i18n label (`status.<key>`); anything else shows the raw value. */
export function statusLabelKey(status: string): string | null {
  const s = status.toLowerCase()
  if (s === 'completed' || s === 'ok' || s === 'success') return 'status.completed'
  if (s === 'error' || s === 'failed') return 'status.failed'
  if (s === 'running') return 'status.running'
  return null
}

/** "2 hours ago" in the UI language; falls back to the date for old entries. */
export function formatRelative(dateStr: string, locale: string, now: number = Date.now()): string {
  const then = new Date(dateStr).getTime()
  if (Number.isNaN(then)) return '—'
  const diffSec = Math.round((then - now) / 1000)
  const abs = Math.abs(diffSec)
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
  if (abs < 60) return rtf.format(Math.round(diffSec / 1) , 'second')
  if (abs < 3600) return rtf.format(Math.round(diffSec / 60), 'minute')
  if (abs < 86400) return rtf.format(Math.round(diffSec / 3600), 'hour')
  if (abs < 86400 * 30) return rtf.format(Math.round(diffSec / 86400), 'day')
  return new Date(then).toLocaleDateString(locale)
}

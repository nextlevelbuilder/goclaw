export type DayGroup = 'today' | 'yesterday' | 'earlier'

const MINUTE = 60_000
const HOUR = 60 * MINUTE
const DAY = 24 * HOUR

/** "4 minutes ago", "yesterday": locale-aware, so it follows the app language. */
export function relativeTime(ms: number, locale: string, now = Date.now()): string {
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto', style: 'short' })
  const diff = ms - now
  const abs = Math.abs(diff)
  if (abs < MINUTE) return rtf.format(0, 'second')
  if (abs < HOUR) return rtf.format(Math.round(diff / MINUTE), 'minute')
  if (abs < DAY) return rtf.format(Math.round(diff / HOUR), 'hour')
  if (abs < 30 * DAY) return rtf.format(Math.round(diff / DAY), 'day')
  return new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric' }).format(new Date(ms))
}

function startOfDay(ms: number): number {
  const d = new Date(ms)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

/** Which list group a timestamp belongs to, by calendar day. */
export function dayGroup(ms: number, now = Date.now()): DayGroup {
  const today = startOfDay(now)
  if (ms >= today) return 'today'
  if (ms >= today - DAY) return 'yesterday'
  return 'earlier'
}

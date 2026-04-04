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

export function statusClass(status: string): string {
  const s = status.toLowerCase()
  if (s === 'completed' || s === 'ok' || s === 'success') {
    return 'bg-emerald-500/15 text-emerald-700 border-emerald-500/25 dark:text-emerald-400'
  }
  if (s === 'error' || s === 'failed') {
    return 'bg-red-500/15 text-red-700 border-red-500/25 dark:text-red-400'
  }
  return 'bg-blue-500/15 text-blue-700 border-blue-500/25 dark:text-blue-400'
}

export function spanTypeIcon(spanType: string): string {
  switch (spanType) {
    case 'llm_call': return '🤖'
    case 'tool_call': return '🔧'
    case 'agent': return '👤'
    case 'embedding': return '📊'
    default: return '📌'
  }
}

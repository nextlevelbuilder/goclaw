// Trace table row with status badge, duration, token counts, and relative time.

import { useTranslation } from 'react-i18next'
import type { TraceData } from '../../types/trace'
import { formatDuration, formatTokens, statusClass } from './trace-detail-formatters'

export function formatRelativeTime(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime()
  if (diff < 60000) return 'just now'
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
  return `${Math.floor(diff / 86400000)}d ago`
}

interface TraceListRowProps {
  trace: TraceData
  onClick: () => void
}

export function TraceListRow({ trace, onClick }: TraceListRowProps) {
  const { t } = useTranslation('traces')
  const cacheRead = trace.metadata?.total_cache_read_tokens as number | undefined
  const nameDisplay = trace.name.length > 30 ? trace.name.slice(0, 30) + '…' : trace.name

  return (
    <tr
      onClick={onClick}
      className="border-b border-border last:border-0 hover:bg-surface-tertiary/30 transition-colors cursor-pointer [&>td]:align-middle"
    >
      {/* Name */}
      <td className="px-4 py-3">
        <div className="flex items-center gap-1.5 min-w-0">
          <span className="text-sm text-text-primary truncate">{nameDisplay || t('unnamed')}</span>
          {trace.parent_trace_id && (
            <span title="Delegated" className="text-text-muted text-xs shrink-0">🔀</span>
          )}
          {trace.channel && (
            <span className="rounded-full px-1.5 py-0.5 text-[10px] bg-surface-tertiary text-text-secondary border border-border shrink-0">
              {trace.channel}
            </span>
          )}
        </div>
      </td>

      {/* Status */}
      <td className="px-4 py-3">
        <span className={`rounded-full px-2 py-0.5 text-[10px] font-medium border ${statusClass(trace.status)}`}>
          {trace.status}
        </span>
      </td>

      {/* Duration */}
      <td className="px-4 py-3 text-xs text-text-muted">
        {formatDuration(trace.duration_ms, trace.start_time, trace.end_time)}
      </td>

      {/* Tokens */}
      <td className="px-4 py-3">
        <div className="font-mono text-xs text-text-primary">
          {formatTokens(trace.total_input_tokens)} / {formatTokens(trace.total_output_tokens)}
        </div>
        {(cacheRead ?? 0) > 0 && (
          <div className="text-[11px] text-emerald-600 dark:text-emerald-400">
            {formatTokens(cacheRead)} {t('cached')}
          </div>
        )}
      </td>

      {/* Spans */}
      <td className="px-4 py-3 text-xs text-text-muted text-center">
        {trace.span_count}
      </td>

      {/* Time */}
      <td className="px-4 py-3 text-xs text-text-muted">
        {formatRelativeTime(trace.created_at)}
      </td>
    </tr>
  )
}

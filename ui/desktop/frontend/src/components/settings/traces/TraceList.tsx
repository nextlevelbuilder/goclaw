import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useTraces } from '../../../hooks/use-traces'
import { useAgentCrud } from '../../../hooks/use-agent-crud'
import { RefreshButton } from '../../common/RefreshButton'
import { Combobox } from '../../common/Combobox'
import { TraceDetailDialog } from './TraceDetailDialog'
import type { TraceData } from '../../../types/trace'

function formatDuration(ms: number | undefined | null, startTime?: string, endTime?: string): string {
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

function formatTokens(count: number | null | undefined): string {
  if (count == null) return '0'
  if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`
  if (count >= 1_000) return `${(count / 1_000).toFixed(1)}K`
  return count.toString()
}

function formatRelativeTime(dateStr: string): string {
  const diff = Date.now() - new Date(dateStr).getTime()
  if (diff < 60000) return 'just now'
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
  return `${Math.floor(diff / 86400000)}d ago`
}

function statusClass(status: string): string {
  const s = status.toLowerCase()
  if (s === 'completed' || s === 'ok' || s === 'success') {
    return 'bg-emerald-500/15 text-emerald-700 border-emerald-500/25 dark:text-emerald-400 dark:bg-emerald-500/10 dark:border-emerald-500/20'
  }
  if (s === 'error' || s === 'failed') {
    return 'bg-red-500/15 text-red-700 border-red-500/25 dark:text-red-400 dark:bg-red-500/10 dark:border-red-500/20'
  }
  return 'bg-blue-500/15 text-blue-700 border-blue-500/25 dark:text-blue-400 dark:bg-blue-500/10 dark:border-blue-500/20'
}

function TraceRow({ trace, onClick }: { trace: TraceData; onClick: () => void }) {
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

export function TraceList() {
  const { t } = useTranslation('traces')
  const { traces, total, loading, fetchTraces, agentFilter, setAgentFilter, loadMore } = useTraces()
  const { agents } = useAgentCrud()
  const [selectedTraceId, setSelectedTraceId] = useState<string | null>(null)

  const agentOptions = [
    { value: '', label: t('allAgents') },
    ...agents.map((a) => ({ value: a.id, label: a.display_name || a.agent_key })),
  ]

  const allLoaded = traces.length >= total && total > 0

  return (
    <div className="space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold text-text-primary">{t('title')}</h2>
          <p className="text-xs text-text-muted mt-0.5">{t('description')}</p>
        </div>
        <div className="flex items-center gap-2">
          <div className="w-44">
            <Combobox
              value={agentFilter}
              onChange={setAgentFilter}
              options={agentOptions}
              placeholder={t('allAgents')}
              allowCustom={false}
            />
          </div>
          <RefreshButton onRefresh={fetchTraces} />
        </div>
      </div>

      {/* Loading */}
      {loading && traces.length === 0 ? (
        <div className="space-y-2">
          {[1, 2, 3].map((i) => (
            <div key={i} className="h-12 rounded-lg bg-surface-tertiary/50 animate-pulse" />
          ))}
        </div>
      ) : traces.length === 0 ? (
        <div className="flex flex-col items-center gap-2 py-12">
          <svg className="h-10 w-10 text-text-muted/40" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={1.5} strokeLinecap="round" strokeLinejoin="round">
            <path d="M22 12h-2.48a2 2 0 0 0-1.93 1.46l-2.35 8.36a.25.25 0 0 1-.48 0L9.24 2.18a.25.25 0 0 0-.48 0l-2.35 8.36A2 2 0 0 1 4.49 12H2" />
          </svg>
          <p className="text-sm text-text-muted">{t('emptyTitle')}</p>
          <p className="text-xs text-text-muted/70">{t('emptyDescription')}</p>
        </div>
      ) : (
        <>
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="w-full text-sm min-w-[600px]">
              <thead>
                <tr className="border-b border-border bg-surface-tertiary/40">
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.name')}</th>
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.status')}</th>
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.duration')}</th>
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.tokens')}</th>
                  <th className="px-4 py-2.5 text-center text-xs font-medium text-text-muted">{t('columns.spans')}</th>
                  <th className="px-4 py-2.5 text-left text-xs font-medium text-text-muted">{t('columns.time')}</th>
                </tr>
              </thead>
              <tbody>
                {traces.map((trace) => (
                  <TraceRow
                    key={trace.id}
                    trace={trace}
                    onClick={() => setSelectedTraceId(trace.id)}
                  />
                ))}
              </tbody>
            </table>
          </div>

          {/* Load more */}
          {!allLoaded && (
            <div className="flex justify-center pt-1">
              <button
                onClick={loadMore}
                disabled={loading}
                className="text-xs text-text-muted hover:text-text-primary transition-colors disabled:opacity-50 flex items-center gap-1.5"
              >
                {loading ? (
                  <svg className="h-3.5 w-3.5 animate-spin" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                    <path d="M21 12a9 9 0 1 1-6.219-8.56" />
                  </svg>
                ) : (
                  <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
                    <path d="M5 12h14" /><path d="m12 5 7 7-7 7" />
                  </svg>
                )}
                Load more ({traces.length}/{total})
              </button>
            </div>
          )}
        </>
      )}

      {/* Detail dialog */}
      {selectedTraceId && (
        <TraceDetailDialog
          traceId={selectedTraceId}
          onClose={() => setSelectedTraceId(null)}
        />
      )}
    </div>
  )
}

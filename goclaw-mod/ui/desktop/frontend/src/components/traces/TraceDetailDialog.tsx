import { useEffect, useState, useMemo, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from '../../stores/toast-store'
import { fetchTraceDetail } from '../../hooks/use-traces'
import { getApiClient, isApiClientReady } from '../../lib/api'
import { DownloadURL } from '../../../wailsjs/go/main/App'
import type { TraceData, SpanData } from '../../types/trace'
import { formatDuration, formatTokens, statusClass } from './trace-detail-formatters'
import { buildSpanTree, SpanRow } from './trace-span-tree'

interface Props {
  traceId: string
  onClose: () => void
}

export function TraceDetailDialog({ traceId, onClose }: Props) {
  const { t } = useTranslation('traces')
  const [trace, setTrace] = useState<TraceData | null>(null)
  const [spans, setSpans] = useState<SpanData[]>([])
  const [loading, setLoading] = useState(true)
  const [expandedSpans, setExpandedSpans] = useState<Set<string>>(new Set())
  const [inputOpen, setInputOpen] = useState(false)
  const [outputOpen, setOutputOpen] = useState(false)
  const [copied, setCopied] = useState(false)

  const handleCopy = useCallback(() => {
    if (!trace) return
    navigator.clipboard.writeText(trace.id)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }, [trace])

  const handleExport = useCallback(async () => {
    if (!trace || !isApiClientReady()) return
    try {
      const exportUrl = `${getApiClient().getBaseUrl()}/v1/traces/${trace.id}/export`
      const filename = `trace-${(trace.name || trace.id.slice(0, 8)).replace(/[^a-zA-Z0-9_-]/g, '_')}.json.gz`
      await DownloadURL(exportUrl, filename)
      toast.success(t('detail.exported'))
    } catch (err) {
      toast.error('Export failed', (err as Error).message)
    }
  }, [trace, t])

  useEffect(() => {
    setLoading(true)
    fetchTraceDetail(traceId)
      .then(({ trace: t2, spans: s }) => {
        setTrace(t2)
        setSpans(s)
        // Auto-expand root spans
        const roots = new Set<string>()
        for (const sp of s) {
          if (!sp.parent_span_id) roots.add(sp.id)
        }
        setExpandedSpans(roots)
      })
      .catch((err) => {
        console.error('Failed to load trace detail:', err)
        toast.error('Failed to load trace', (err as Error).message)
      })
      .finally(() => setLoading(false))
  }, [traceId])

  const spanTree = useMemo(() => buildSpanTree(spans), [spans])
  const cacheRead = trace?.metadata?.total_cache_read_tokens as number | undefined

  const toggleSpan = (id: string) => {
    setExpandedSpans((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  return (
    <div className="fixed inset-0 z-[70] flex items-center justify-center">
      <div className="absolute inset-0 bg-black/50" onClick={onClose} />
      <div className="relative w-full max-w-4xl max-h-[85vh] flex flex-col bg-surface-secondary rounded-xl border border-border overflow-hidden mx-4">
        {/* Header */}
        <div className="flex items-center justify-between border-b border-border px-5 py-3 shrink-0">
          <div className="flex items-center gap-2 min-w-0">
            <span className="text-sm font-medium text-text-primary truncate">{trace?.name || t('unnamed')}</span>
            {trace && (
              <span className={`rounded-full px-2 py-0.5 border text-[10px] font-medium shrink-0 ${statusClass(trace.status)}`}>
                {trace.status}
              </span>
            )}
          </div>
          <div className="flex items-center gap-1 shrink-0">
            {trace && (
              <>
                <button
                  onClick={handleCopy}
                  className={`p-1.5 transition-colors cursor-pointer ${copied ? 'text-emerald-500' : 'text-text-muted hover:text-text-primary'}`}
                  title={t('detail.copyTraceId')}
                >
                  {copied ? (
                    <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                      <polyline points="20 6 9 17 4 12" />
                    </svg>
                  ) : (
                    <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                      <rect width="14" height="14" x="8" y="8" rx="2" /><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" />
                    </svg>
                  )}
                </button>
                <button
                  onClick={handleExport}
                  className="p-1.5 text-text-muted hover:text-text-primary transition-colors cursor-pointer"
                  title={t('detail.export')}
                >
                  <svg className="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                    <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" /><polyline points="7 10 12 15 17 10" /><line x1="12" y1="15" x2="12" y2="3" />
                  </svg>
                </button>
              </>
            )}
            <button onClick={onClose} className="p-1 text-text-muted hover:text-text-primary transition-colors cursor-pointer">
              <svg className="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                <path d="M18 6 6 18" /><path d="m6 6 12 12" />
              </svg>
            </button>
          </div>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-y-auto overscroll-contain">
          {loading ? (
            <div className="flex items-center justify-center py-12">
              <svg className="h-5 w-5 animate-spin text-text-muted" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                <path d="M21 12a9 9 0 1 1-6.219-8.56" />
              </svg>
            </div>
          ) : !trace ? (
            <p className="text-sm text-text-muted text-center py-8">{t('detail.notFound')}</p>
          ) : (
            <div className="p-5 space-y-4">
              {/* Metadata */}
              <div className="flex flex-wrap gap-x-4 gap-y-2 text-xs text-text-muted">
                <span><span className="text-text-secondary">{t('detail.duration')}</span> {formatDuration(trace.duration_ms, trace.start_time, trace.end_time)}</span>
                {trace.channel && (
                  <span className="rounded-full px-2 py-0.5 bg-surface-tertiary text-text-secondary border border-border">{trace.channel}</span>
                )}
                <span>
                  <span className="text-text-secondary">{t('detail.tokens')}</span>{' '}
                  {formatTokens(trace.total_input_tokens)} in / {formatTokens(trace.total_output_tokens)} out
                  {(cacheRead ?? 0) > 0 && (
                    <span className="ml-1 text-emerald-600 dark:text-emerald-400">+{formatTokens(cacheRead)} cached</span>
                  )}
                </span>
                <span><span className="text-text-secondary">{t('detail.spans')}</span> {trace.span_count}</span>
                <span>
                  <span className="text-text-secondary">{t('detail.started')}</span>{' '}
                  {new Date(trace.start_time).toLocaleString()}
                </span>
                {trace.parent_trace_id && (
                  <span>
                    <span className="text-text-secondary">{t('detail.delegatedFrom')}</span>{' '}
                    <span className="font-mono text-accent">{trace.parent_trace_id.slice(0, 8)}…</span>
                  </span>
                )}
              </div>

              {/* Trace-level Input/Output */}
              {(trace.input_preview || trace.output_preview) && (
                <div className="space-y-2 border-t border-border pt-3">
                  {trace.input_preview && (
                    <div>
                      <button
                        onClick={() => setInputOpen((v) => !v)}
                        className="flex items-center gap-1.5 text-xs font-medium text-text-secondary hover:text-text-primary transition-colors cursor-pointer"
                      >
                        <svg className={`h-3.5 w-3.5 transition-transform ${inputOpen ? 'rotate-90' : ''}`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                          <path d="M9 18l6-6-6-6" />
                        </svg>
                        {t('detail.input')}
                      </button>
                      {inputOpen && (
                        <div className="mt-1.5 max-h-[40vh] overflow-y-auto overflow-x-hidden">
                          <pre className="text-xs text-text-primary whitespace-pre-wrap">{trace.input_preview}</pre>
                        </div>
                      )}
                    </div>
                  )}
                  {trace.output_preview && (
                    <div>
                      <button
                        onClick={() => setOutputOpen((v) => !v)}
                        className="flex items-center gap-1.5 text-xs font-medium text-text-secondary hover:text-text-primary transition-colors cursor-pointer"
                      >
                        <svg className={`h-3.5 w-3.5 transition-transform ${outputOpen ? 'rotate-90' : ''}`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2}>
                          <path d="M9 18l6-6-6-6" />
                        </svg>
                        {t('detail.output')}
                      </button>
                      {outputOpen && (
                        <div className="mt-1.5 max-h-[40vh] overflow-y-auto overflow-x-hidden">
                          <pre className="text-xs text-text-primary whitespace-pre-wrap">{trace.output_preview}</pre>
                        </div>
                      )}
                    </div>
                  )}
                </div>
              )}

              {/* Span tree */}
              {spanTree.length > 0 && (
                <div className="border-t border-border pt-3">
                  <p className="text-xs font-medium text-text-secondary mb-2">
                    {t('detail.spansCount', { count: spans.length })}
                  </p>
                  <div className="rounded-lg border border-border overflow-hidden">
                    {spanTree.map((node) => (
                      <SpanRow
                        key={node.span.id}
                        node={node}
                        expanded={expandedSpans.has(node.span.id)}
                        onToggle={() => toggleSpan(node.span.id)}
                      />
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

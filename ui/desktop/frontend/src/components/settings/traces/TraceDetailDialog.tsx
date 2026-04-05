import { useEffect, useState, useMemo, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from '../../../stores/toast-store'
import { fetchTraceDetail } from '../../../hooks/use-traces'
import { getApiClient, isApiClientReady } from '../../../lib/api'
import { DownloadURL } from '../../../../wailsjs/go/main/App'
import type { TraceData, SpanData } from '../../../types/trace'

interface Props {
  traceId: string
  onClose: () => void
}

import { MarkdownRenderer } from '../../chat/MarkdownRenderer'

function TraceContentPreview({ text }: { text?: string }) {
  if (!text) return null
  const trimmed = text.trim()
  // Auto-format JSON into a markdown code block
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try {
      const formatted = JSON.stringify(JSON.parse(trimmed), null, 2)
      return (
        <div className="max-h-[40vh] overflow-y-auto overflow-x-hidden">
          <MarkdownRenderer content={'```json\n' + formatted + '\n```'} />
        </div>
      )
    } catch { /* not valid JSON, fall through */ }
  }
  // Render as markdown (handles code blocks, headings, lists, etc.)
  return (
    <div className="max-h-[40vh] overflow-y-auto overflow-x-hidden">
      <MarkdownRenderer content={text} />
    </div>
  )
}

function formatDuration(ms: number | undefined | null, startTime?: string, endTime?: string): string {
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

function formatTokens(count: number | null | undefined): string {
  if (count == null) return '0'
  if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M`
  if (count >= 1_000) return `${(count / 1_000).toFixed(1)}K`
  return count.toString()
}

function statusClass(status: string): string {
  const s = status.toLowerCase()
  if (s === 'completed' || s === 'ok' || s === 'success') {
    return 'bg-emerald-500/15 text-emerald-700 border-emerald-500/25 dark:text-emerald-400'
  }
  if (s === 'error' || s === 'failed') {
    return 'bg-red-500/15 text-red-700 border-red-500/25 dark:text-red-400'
  }
  return 'bg-blue-500/15 text-blue-700 border-blue-500/25 dark:text-blue-400'
}

function spanTypeIcon(spanType: string): string {
  switch (spanType) {
    case 'llm_call': return '🤖'
    case 'tool_call': return '🔧'
    case 'agent': return '👤'
    case 'embedding': return '📊'
    default: return '📌'
  }
}

// Build span tree from flat list using parent_span_id
interface SpanNode { span: SpanData; children: SpanNode[]; depth: number }

function buildSpanTree(spans: SpanData[]): SpanNode[] {
  const byId = new Map<string, SpanNode>()
  const roots: SpanNode[] = []
  for (const span of spans) {
    byId.set(span.id, { span, children: [], depth: 0 })
  }
  for (const span of spans) {
    const node = byId.get(span.id)!
    const parentId = span.parent_span_id
    if (parentId && byId.has(parentId)) {
      const parent = byId.get(parentId)!
      node.depth = parent.depth + 1
      parent.children.push(node)
    } else {
      roots.push(node)
    }
  }
  // Flatten tree in DFS order
  const flat: SpanNode[] = []
  function walk(nodes: SpanNode[]) {
    for (const n of nodes) { flat.push(n); walk(n.children) }
  }
  walk(roots)
  return flat
}

function SpanRow({ node, expanded, onToggle }: { node: SpanNode; expanded: boolean; onToggle: () => void }) {
  const { t } = useTranslation('traces')
  const { span, depth } = node
  const hasTokens = (span.input_tokens ?? 0) > 0 || (span.output_tokens ?? 0) > 0
  const subtitle = span.span_type === 'llm_call'
    ? [span.model, span.provider].filter(Boolean).join(' / ')
    : span.span_type === 'tool_call' ? span.tool_name : undefined
  const cacheRead = (span.metadata?.cache_read_tokens as number) ?? 0
  const cacheCreate = (span.metadata?.cache_creation_tokens as number) ?? 0
  const thinkingTokens = (span.metadata?.thinking_tokens as number) ?? 0
  // Expandable only if there's content to show
  const hasExpandContent = !!span.input_preview || !!span.output_preview || !!span.error || cacheCreate > 0

  return (
    <div className="border-b border-border last:border-0">
      {/* Row */}
      <button
        type="button"
        onClick={hasExpandContent ? onToggle : undefined}
        className={`w-full flex items-center gap-1.5 px-3 py-2 text-left transition-colors ${hasExpandContent ? 'hover:bg-surface-tertiary/30 cursor-pointer' : ''}`}
        style={{ paddingLeft: `${12 + depth * 16}px` }}
      >
        {/* Toggle arrow — only visible if expandable */}
        <svg
          className={`h-3 w-3 shrink-0 text-text-muted transition-transform ${expanded ? 'rotate-90' : ''} ${!hasExpandContent ? 'invisible' : ''}`}
          viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2.5}
        >
          <path d="M9 18l6-6-6-6" />
        </svg>
        <span className="text-sm shrink-0">{spanTypeIcon(span.span_type)}</span>
        <span className="text-xs font-medium text-text-primary truncate min-w-0 flex-1">{span.name}</span>
        {subtitle && <span className="text-[10px] text-text-muted truncate max-w-[120px] shrink-0">{subtitle}</span>}
        <div className="flex items-center gap-2 shrink-0 text-[11px] text-text-muted ml-auto">
          {hasTokens && (
            <span className="font-mono">
              {formatTokens(span.input_tokens)}/{formatTokens(span.output_tokens)}
              {cacheRead > 0 && <span className="ml-1 text-emerald-600 dark:text-emerald-400">({formatTokens(cacheRead)} cached)</span>}
              {thinkingTokens > 0 && <span className="ml-1 text-orange-600 dark:text-orange-400">({formatTokens(thinkingTokens)} thinking)</span>}
            </span>
          )}
          <span>{formatDuration(span.duration_ms, span.start_time, span.end_time)}</span>
          <span className={`rounded-full px-1.5 py-0.5 border text-[10px] font-medium ${statusClass(span.status)}`}>
            {span.status}
          </span>
        </div>
      </button>

      {/* Expanded details — only input/output/error, no duplicate metadata */}
      {expanded && hasExpandContent && (
        <div className="px-4 pb-3 space-y-2" style={{ paddingLeft: `${28 + depth * 16}px` }}>
          {/* Cache breakdown (only shows extra info not in collapsed row) */}
          {cacheCreate > 0 && (
            <div className="text-[11px]">
              <span className="text-yellow-600 dark:text-yellow-400">+{formatTokens(cacheCreate)} cache write</span>
            </div>
          )}
          {span.error && <p className="text-[11px] text-red-600 dark:text-red-400">{span.error}</p>}
          {span.input_preview && (
            <div>
              <p className="text-[11px] font-medium text-text-secondary mb-1">{t('detail.input')}</p>
              <TraceContentPreview text={span.input_preview} />
            </div>
          )}
          {span.output_preview && (
            <div>
              <p className="text-[11px] font-medium text-text-secondary mb-1">{t('detail.output')}</p>
              <TraceContentPreview text={span.output_preview} />
            </div>
          )}
        </div>
      )}
    </div>
  )
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
                {/* Copy trace ID */}
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
                {/* Export trace */}
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
                      {inputOpen && <div className="mt-1.5"><TraceContentPreview text={trace.input_preview} /></div>}
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
                      {outputOpen && <div className="mt-1.5"><TraceContentPreview text={trace.output_preview} /></div>}
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

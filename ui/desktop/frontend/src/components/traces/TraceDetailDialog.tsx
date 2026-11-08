import { useEffect, useState, useMemo, useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from '../../stores/toast-store'
import { fetchTraceDetail } from '../../hooks/use-traces'
import { getApiClient, isApiClientReady } from '../../lib/api'
import { wails } from '../../lib/wails'
import { IconCheck, IconChevronDown, IconDocument, IconDownload } from '../common/Icons'
import { Dialog, ErrorState, IconButton, Skeleton, StatusChip } from '../ui'
import type { TraceData, SpanData } from '../../types/trace'
import { formatDuration, formatTokens, statusLabelKey, statusTone } from './trace-detail-formatters'
import { buildSpanTree, SpanRow } from './trace-span-tree'

interface Props {
  traceId: string
  onClose: () => void
}

function CollapsibleText({ label, text }: { label: string; text: string }) {
  const [open, setOpen] = useState(false)
  return (
    <div>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1.5 rounded-sm text-ui text-ink-secondary transition-colors hover:text-ink"
      >
        <IconChevronDown size={14} className={open ? '' : '-rotate-90'} />
        {label}
      </button>
      {open && (
        <pre className="mt-1.5 max-h-[40vh] overflow-y-auto overflow-x-hidden whitespace-pre-wrap rounded-md bg-surface-base p-3 text-code text-ink">{text}</pre>
      )}
    </div>
  )
}

export function TraceDetailDialog({ traceId, onClose }: Props) {
  const { t } = useTranslation(['traces', 'common'])
  const [trace, setTrace] = useState<TraceData | null>(null)
  const [spans, setSpans] = useState<SpanData[]>([])
  const [loading, setLoading] = useState(true)
  const [failed, setFailed] = useState(false)
  const [expandedSpans, setExpandedSpans] = useState<Set<string>>(new Set())
  const [copied, setCopied] = useState(false)
  const [reloadKey, setReloadKey] = useState(0)

  const handleCopy = useCallback(() => {
    if (!trace) return
    void navigator.clipboard.writeText(trace.id)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }, [trace])

  const handleExport = useCallback(async () => {
    if (!trace || !isApiClientReady()) return
    try {
      const exportUrl = `${getApiClient().getBaseUrl()}/v1/traces/${trace.id}/export`
      const filename = `trace-${(trace.name || trace.id.slice(0, 8)).replace(/[^a-zA-Z0-9_-]/g, '_')}.json.gz`
      await wails.downloadURL(exportUrl, filename)
      toast.success(t('traces:detail.exported'))
    } catch (err) {
      toast.error(t('traces:detail.exportFailed'), (err as Error).message)
    }
  }, [trace, t])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setFailed(false)
    fetchTraceDetail(traceId)
      .then(({ trace: loaded, spans: s }) => {
        if (cancelled) return
        setTrace(loaded)
        setSpans(s)
        // Auto-expand root spans
        setExpandedSpans(new Set(s.filter((sp) => !sp.parent_span_id).map((sp) => sp.id)))
      })
      .catch((err) => {
        if (cancelled) return
        console.error('Failed to load trace detail:', err)
        setFailed(true)
      })
      .finally(() => { if (!cancelled) setLoading(false) })
    return () => { cancelled = true }
  }, [traceId, reloadKey])

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

  const statusKey = trace ? statusLabelKey(trace.status) : null

  return (
    <Dialog
      open
      onOpenChange={(open) => { if (!open) onClose() }}
      size="xl"
      title={trace?.name || t('traces:unnamed')}
      description={trace ? new Date(trace.start_time).toLocaleString() : undefined}
    >
      {loading ? (
        <div role="status" aria-busy="true" aria-label={t('common:state.loading')} className="flex flex-col gap-3 py-2">
          <Skeleton className="h-4 w-2/3" />
          <Skeleton className="h-4 w-1/2" />
          <Skeleton className="h-24 w-full" />
        </div>
      ) : failed ? (
        <ErrorState
          title={t('traces:states.detailErrorTitle')}
          description={t('traces:states.errorBody')}
          onRetry={() => setReloadKey((k) => k + 1)}
        />
      ) : !trace ? (
        <p className="py-8 text-center text-ui text-ink-muted">{t('traces:detail.notFound')}</p>
      ) : (
        <div className="flex flex-col gap-4">
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-caption text-ink-muted">
            <StatusChip tone={statusTone(trace.status)} label={statusKey ? t(`traces:${statusKey}`) : trace.status} />
            <span><span className="text-ink-secondary">{t('traces:detail.duration')}</span> {formatDuration(trace.duration_ms, trace.start_time, trace.end_time)}</span>
            {trace.channel && (
              <span className="rounded-pill border border-border-subtle px-2 text-ink-secondary">{trace.channel}</span>
            )}
            <span>
              <span className="text-ink-secondary">{t('traces:detail.tokens')}</span>{' '}
              {t('traces:detail.tokensInOut', { input: formatTokens(trace.total_input_tokens), output: formatTokens(trace.total_output_tokens) })}
              {(cacheRead ?? 0) > 0 && <span className="ml-1 text-success">+{formatTokens(cacheRead)} {t('traces:cached')}</span>}
            </span>
            <span><span className="text-ink-secondary">{t('traces:detail.spans')}</span> {trace.span_count}</span>
            {trace.parent_trace_id && (
              <span>
                <span className="text-ink-secondary">{t('traces:detail.delegatedFrom')}</span>{' '}
                <span className="text-code text-accent-subtle-ink">{trace.parent_trace_id.slice(0, 8)}…</span>
              </span>
            )}
            <span className="ml-auto flex items-center gap-1">
              <IconButton
                aria-label={t('traces:detail.copyTraceId')}
                icon={copied ? <IconCheck size={14} /> : <IconDocument size={14} />}
                size="sm"
                onClick={handleCopy}
              />
              <IconButton aria-label={t('traces:detail.export')} icon={<IconDownload size={14} />} size="sm" onClick={() => { void handleExport() }} />
            </span>
          </div>

          {trace.error && (
            <p role="alert" className="rounded-md bg-danger-subtle px-3 py-2 text-caption text-danger">{trace.error}</p>
          )}

          {(trace.input_preview || trace.output_preview) && (
            <div className="flex flex-col gap-2 border-t border-border-subtle pt-3">
              {trace.input_preview && <CollapsibleText label={t('traces:detail.input')} text={trace.input_preview} />}
              {trace.output_preview && <CollapsibleText label={t('traces:detail.output')} text={trace.output_preview} />}
            </div>
          )}

          {spanTree.length > 0 && (
            <div className="border-t border-border-subtle pt-3">
              <p className="mb-2 text-ui text-ink-secondary">{t('traces:detail.spansCount', { count: spans.length })}</p>
              <div className="overflow-hidden rounded-lg border border-border-subtle">
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
    </Dialog>
  )
}

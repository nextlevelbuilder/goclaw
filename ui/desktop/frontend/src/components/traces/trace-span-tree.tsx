// Span tree rendering — builds and displays hierarchical trace spans.

import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { SpanData } from '../../types/trace'
import { formatDuration, formatTokens, statusClass, spanTypeIcon } from './trace-detail-formatters'

// Build span tree from flat list using parent_span_id
export interface SpanNode { span: SpanData; children: SpanNode[]; depth: number }

export function buildSpanTree(spans: SpanData[]): SpanNode[] {
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

function TraceContentPreview({ text }: { text?: string }) {
  const [copied, setCopied] = useState(false)
  if (!text) return null
  const trimmed = text.trim()
  // Pretty-print JSON if valid
  let display = trimmed
  let lang = ''
  if (trimmed.startsWith('{') || trimmed.startsWith('[')) {
    try { display = JSON.stringify(JSON.parse(trimmed), null, 2); lang = 'json' } catch { /* not valid JSON */ }
  }
  const handleCopy = () => {
    navigator.clipboard.writeText(display)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }
  return (
    <div className="rounded-lg border border-border overflow-hidden">
      {lang && (
        <div className="flex items-center justify-between px-3 py-1 bg-surface-tertiary text-[11px]">
          <span className="text-text-muted font-mono">{lang}</span>
          <button onClick={handleCopy} className="text-text-muted hover:text-text-primary transition-colors cursor-pointer">
            {copied ? 'Copied!' : 'Copy'}
          </button>
        </div>
      )}
      <pre className="text-xs text-text-primary whitespace-pre-wrap p-3 max-h-[40vh] overflow-y-auto overscroll-contain">{display}</pre>
    </div>
  )
}

export function SpanRow({ node, expanded, onToggle }: { node: SpanNode; expanded: boolean; onToggle: () => void }) {
  const { t } = useTranslation('traces')
  const { span, depth } = node
  const hasTokens = (span.input_tokens ?? 0) > 0 || (span.output_tokens ?? 0) > 0
  const subtitle = span.span_type === 'llm_call'
    ? [span.model, span.provider].filter(Boolean).join(' / ')
    : span.span_type === 'tool_call' ? span.tool_name : undefined
  const cacheRead = (span.metadata?.cache_read_tokens as number) ?? 0
  const cacheCreate = (span.metadata?.cache_creation_tokens as number) ?? 0
  const thinkingTokens = (span.metadata?.thinking_tokens as number) ?? 0
  const hasExpandContent = !!span.input_preview || !!span.output_preview || !!span.error || cacheCreate > 0

  return (
    <div className="border-b border-border last:border-0">
      <button
        type="button"
        onClick={hasExpandContent ? onToggle : undefined}
        className={`w-full flex items-center gap-1.5 px-3 py-2 text-left transition-colors ${hasExpandContent ? 'hover:bg-surface-tertiary/30 cursor-pointer' : ''}`}
        style={{ paddingLeft: `${12 + depth * 16}px` }}
      >
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

      {expanded && hasExpandContent && (
        <div className="px-4 pb-3 space-y-2" style={{ paddingLeft: `${28 + depth * 16}px` }}>
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

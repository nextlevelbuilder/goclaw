// Span tree rendering — builds and displays hierarchical trace spans.

import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { SpanData } from '../../types/trace'
import { IconActivity, IconBarChart, IconBot, IconChat, IconChevronDown, IconDot, IconWrench } from '../common/Icons'
import { StatusChip } from '../ui'
import { formatDuration, formatTokens, statusLabelKey, statusTone } from './trace-detail-formatters'

function SpanTypeIcon({ type }: { type: string }) {
  const props = { size: 14, className: 'shrink-0 text-ink-muted' }
  switch (type) {
    case 'llm_call': return <IconChat {...props} />
    case 'tool_call': return <IconWrench {...props} />
    case 'agent': return <IconBot {...props} />
    case 'embedding': return <IconBarChart {...props} />
    case 'event': return <IconActivity {...props} />
    default: return <IconDot {...props} />
  }
}

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
  const { t } = useTranslation('traces')
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
    <div className="rounded-lg border border-border-subtle overflow-hidden">
      {lang && (
        <div className="flex items-center justify-between px-3 py-1 bg-surface-hover text-caption">
          <span className="text-ink-muted font-mono">{lang}</span>
          <button onClick={handleCopy} className="text-ink-muted hover:text-ink transition-colors cursor-pointer">
            {copied ? t('span.copied') : t('span.copy')}
          </button>
        </div>
      )}
      <pre className="text-caption text-ink whitespace-pre-wrap p-3 max-h-[40vh] overflow-y-auto overscroll-contain">{display}</pre>
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
    <div className="border-b border-border-subtle last:border-0">
      <button
        type="button"
        onClick={hasExpandContent ? onToggle : undefined}
        className={`w-full flex items-center gap-1.5 px-3 py-2 text-left transition-colors ${hasExpandContent ? 'hover:bg-surface-hover cursor-pointer' : ''}`}
        style={{ paddingLeft: `${12 + depth * 16}px` }}
      >
        <IconChevronDown
          size={12}
          className={`shrink-0 text-ink-muted transition-transform ${expanded ? '' : '-rotate-90'} ${!hasExpandContent ? 'invisible' : ''}`}
        />
        <SpanTypeIcon type={span.span_type} />
        <span className="text-caption font-medium text-ink truncate min-w-0 flex-1">{span.name}</span>
        {subtitle && <span className="text-caption text-ink-muted truncate max-w-[120px] shrink-0">{subtitle}</span>}
        <div className="flex items-center gap-2 shrink-0 text-caption text-ink-muted ml-auto">
          {hasTokens && (
            <span className="font-mono">
              {formatTokens(span.input_tokens)}/{formatTokens(span.output_tokens)}
              {cacheRead > 0 && <span className="ml-1 text-success">({formatTokens(cacheRead)} {t('span.cached')})</span>}
              {thinkingTokens > 0 && <span className="ml-1 text-warning">({formatTokens(thinkingTokens)} {t('span.thinking')})</span>}
            </span>
          )}
          <span>{formatDuration(span.duration_ms, span.start_time, span.end_time)}</span>
          <StatusChip tone={statusTone(span.status)} label={statusLabelKey(span.status) ? t(statusLabelKey(span.status)!) : span.status} />
        </div>
      </button>

      {expanded && hasExpandContent && (
        <div className="px-4 pb-3 space-y-2" style={{ paddingLeft: `${28 + depth * 16}px` }}>
          {cacheCreate > 0 && (
            <div className="text-caption">
              <span className="text-warning">+{formatTokens(cacheCreate)} {t('span.cacheWrite')}</span>
            </div>
          )}
          {span.error && <p className="text-caption text-danger">{span.error}</p>}
          {span.input_preview && (
            <div>
              <p className="text-caption font-medium text-ink-secondary mb-1">{t('detail.input')}</p>
              <TraceContentPreview text={span.input_preview} />
            </div>
          )}
          {span.output_preview && (
            <div>
              <p className="text-caption font-medium text-ink-secondary mb-1">{t('detail.output')}</p>
              <TraceContentPreview text={span.output_preview} />
            </div>
          )}
        </div>
      )}
    </div>
  )
}

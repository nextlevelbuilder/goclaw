import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { ToolCall } from '../../stores/chat-store'
import { isToolDenied } from '../../utils/chat-error'
import { cn } from '../../lib/cn'
import { IconActivity, IconWrench } from '../common/Icons'
import { StatusChip, type StatusTone } from '../ui'
import { IconChevronRight } from './chat-icons'

const isSkillTool = (name: string) => name === 'use_skill'

/** Extract first meaningful arg (path/command/query/url/name), truncate to 77 chars. */
function buildToolSummary(args: Record<string, unknown>): string | null {
  const key = args.path ?? args.command ?? args.query ?? args.url ?? args.name
  if (typeof key === 'string') return key.length > 80 ? key.slice(0, 77) + '...' : key
  return null
}

interface ToolCallBlockProps {
  toolCall: ToolCall
}

/** One tool call as a single `text-code` row; click to expand arguments and result in place. */
export function ToolCallBlock({ toolCall }: ToolCallBlockProps) {
  const { t } = useTranslation('chat')
  const [expanded, setExpanded] = useState(false)
  const skill = isSkillTool(toolCall.toolName)
  const displayName = skill
    ? t('thread.skill', { name: (toolCall.arguments?.name as string) || 'unknown' })
    : toolCall.toolName
  const summary = buildToolSummary(toolCall.arguments)
  const hasArgs = Object.keys(toolCall.arguments).length > 0
  const canExpand = hasArgs || !!toolCall.result || !!toolCall.error

  let tone: StatusTone = 'info'
  let label = t('thread.toolRunning')
  if (toolCall.state === 'completed') {
    tone = 'success'
    label = t('thread.toolDone')
  }
  if (toolCall.state === 'error') {
    tone = 'danger'
    label = isToolDenied(toolCall.error) ? t('thread.toolDenied') : t('thread.toolFailed')
  }
  const Icon = skill ? IconActivity : IconWrench

  return (
    <div className="min-w-0 rounded-sm border border-border-subtle bg-surface-raised">
      <button
        type="button"
        onClick={() => canExpand && setExpanded((v) => !v)}
        disabled={!canExpand}
        aria-expanded={canExpand ? expanded : undefined}
        className="flex h-8 w-full items-center gap-2 rounded-sm px-2.5 text-left text-code text-ink-secondary transition-colors enabled:hover:bg-surface-hover"
      >
        <Icon size={14} className="shrink-0 text-ink-muted" />
        <span className="shrink-0 text-ink">{displayName}</span>
        {summary && <span className="min-w-0 flex-1 truncate text-ink-muted">{summary}</span>}
        <span className={cn('flex shrink-0 items-center gap-2', !summary && 'ml-auto')}>
          <StatusChip tone={tone} label={label} />
          {canExpand && <IconChevronRight size={12} className={cn('text-ink-muted transition-transform', expanded && 'rotate-90')} />}
        </span>
      </button>

      {expanded && canExpand && (
        <div className="flex flex-col gap-2 border-t border-border-subtle px-2.5 py-2">
          {toolCall.error && <pre className="whitespace-pre-wrap break-words text-code text-danger">{toolCall.error}</pre>}
          {hasArgs && (
            <div>
              <div className="mb-1 text-overline text-ink-muted">{t('thread.arguments')}</div>
              <pre className="max-h-40 overflow-y-auto overscroll-contain whitespace-pre-wrap break-words rounded-sm bg-surface-window p-2 text-code text-ink-secondary">
                {JSON.stringify(toolCall.arguments, null, 2)}
              </pre>
            </div>
          )}
          {toolCall.result && (
            <div>
              <div className="mb-1 text-overline text-ink-muted">{t('thread.result')}</div>
              <pre className="max-h-40 overflow-y-auto overscroll-contain whitespace-pre-wrap break-words rounded-sm bg-surface-window p-2 text-code text-ink-secondary">
                {toolCall.result}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

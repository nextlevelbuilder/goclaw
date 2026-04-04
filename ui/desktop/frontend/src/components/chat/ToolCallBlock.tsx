import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { ToolCall } from '../../stores/chat-store'

// Inline SVG icons (no lucide-react dependency)
function WrenchIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
    </svg>
  )
}

function ZapIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
    </svg>
  )
}

function AlertIcon({ className }: { className?: string }) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
      <path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z" />
      <line x1="12" y1="9" x2="12" y2="13" /><line x1="12" y1="17" x2="12.01" y2="17" />
    </svg>
  )
}

const isSkillTool = (name: string) => name === 'use_skill'

/** Extract first meaningful arg (path/command/query/url/name), truncate to 77 chars. */
function buildToolSummary(args: Record<string, unknown>): string | null {
  const key = args.path ?? args.command ?? args.query ?? args.url ?? args.name
  if (typeof key === 'string') return key.length > 80 ? key.slice(0, 77) + '...' : key
  return null
}

function ToolIcon({ state, isSkill }: { state: ToolCall['state']; isSkill: boolean }) {
  const cls = 'h-3.5 w-3.5 shrink-0'
  if (isSkill) {
    if (state === 'calling') return <ZapIcon className={`${cls} animate-pulse text-amber-500`} />
    if (state === 'error') return <AlertIcon className={`${cls} text-error`} />
    return <ZapIcon className={`${cls} text-amber-500`} />
  }
  if (state === 'calling') return <WrenchIcon className={`${cls} animate-wobble text-blue-500`} />
  if (state === 'error') return <AlertIcon className={`${cls} text-error`} />
  return <WrenchIcon className={`${cls} text-blue-500`} />
}

function PhaseLabel({ state, isSkill }: { state: ToolCall['state']; isSkill: boolean }) {
  const { t } = useTranslation('common')
  const label = isSkill
    ? { calling: t('skillActivating'), completed: t('skillActivated'), error: t('toolFailed') }[state]
    : { calling: t('toolRunning'), completed: t('toolDone'), error: t('toolFailed') }[state]
  const color = state === 'error' ? 'text-error' : state === 'completed' ? 'text-text-secondary' : 'text-blue-500'
  return <span className={`text-[11px] ${color}`}>{label}</span>
}

interface ToolCallBlockProps {
  toolCall: ToolCall
  /** Compact mode — less padding, used inside grouped containers */
  compact?: boolean
}

export function ToolCallBlock({ toolCall, compact }: ToolCallBlockProps) {
  const { t } = useTranslation('common')
  const [expanded, setExpanded] = useState(false)
  const skill = isSkillTool(toolCall.toolName)
  const displayName = skill
    ? `skill: ${(toolCall.arguments?.name as string) || 'unknown'}`
    : toolCall.toolName
  const summary = buildToolSummary(toolCall.arguments)
  const canExpand = Object.keys(toolCall.arguments).length > 0 || toolCall.result || toolCall.error

  return (
    <div className={compact ? '' : 'rounded-md border border-border bg-surface-tertiary/30'}>
      <button
        type="button"
        onClick={() => canExpand && setExpanded(!expanded)}
        disabled={!canExpand}
        className="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs hover:bg-surface-tertiary/50 transition-colors"
      >
        <ToolIcon state={toolCall.state} isSkill={skill} />
        <span className="font-medium text-text-primary shrink-0">{displayName}</span>
        {summary && <span className="truncate text-text-secondary ml-1">{summary}</span>}
        <span className="ml-auto flex items-center gap-1 shrink-0">
          <PhaseLabel state={toolCall.state} isSkill={skill} />
          {canExpand && (
            <svg className={`w-3 h-3 text-text-muted transition-transform ${expanded ? 'rotate-90' : ''}`} fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
            </svg>
          )}
        </span>
      </button>

      {expanded && canExpand && (
        <div className="border-t border-border px-3 py-2 space-y-2">
          {toolCall.error && (
            <pre className="text-xs text-error whitespace-pre-wrap">{toolCall.error}</pre>
          )}
          {Object.keys(toolCall.arguments).length > 0 && (
            <div>
              <div className="text-[10px] font-semibold uppercase text-text-muted mb-0.5">{t('toolArguments')}</div>
              <pre className="whitespace-pre-wrap text-[11px] font-mono text-text-secondary bg-surface-primary rounded p-1.5 max-h-40 overflow-y-auto">
                {JSON.stringify(toolCall.arguments, null, 2)}
              </pre>
            </div>
          )}
          {toolCall.result && (
            <div>
              <div className="text-[10px] font-semibold uppercase text-text-muted mb-0.5">{t('toolResult')}</div>
              <pre className="whitespace-pre-wrap text-[11px] font-mono text-text-secondary bg-surface-primary rounded p-1.5 max-h-40 overflow-y-auto">
                {toolCall.result}
              </pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

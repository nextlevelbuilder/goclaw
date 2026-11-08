import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { cn } from '../../lib/cn'
import { IconClock } from '../common/Icons'
import { IconChevronRight } from './chat-icons'
import './chat-shimmer.css'

interface ThinkingBlockProps {
  text: string
  isStreaming?: boolean
}

/** The model's reasoning as one quiet line that expands in place. */
export function ThinkingBlock({ text, isStreaming }: ThinkingBlockProps) {
  const { t } = useTranslation('chat')
  const [expanded, setExpanded] = useState(false)

  if (!text) return null

  return (
    <div className="min-w-0">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        aria-label={expanded ? t('thread.collapse') : t('thread.expand')}
        className="flex items-center gap-2 rounded-sm text-code text-ink-muted transition-colors hover:text-ink-secondary"
      >
        <IconClock size={14} />
        <span className={cn(isStreaming && 'chat-shimmer')}>{isStreaming ? t('thread.thinking') : t('thread.thought')}</span>
        <IconChevronRight size={12} className={cn('transition-transform', expanded && 'rotate-90')} />
      </button>
      {expanded && (
        <pre className="mt-2 max-h-80 overflow-y-auto overscroll-contain whitespace-pre-wrap break-words border-l border-border-subtle pl-3 text-code text-ink-secondary">
          {text}
        </pre>
      )}
    </div>
  )
}

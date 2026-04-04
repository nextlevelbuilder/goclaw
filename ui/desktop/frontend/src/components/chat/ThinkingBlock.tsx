import { useState, useEffect } from 'react'
import { useTranslation } from 'react-i18next'

interface ThinkingBlockProps {
  text: string
  isStreaming?: boolean
}

export function ThinkingBlock({ text, isStreaming }: ThinkingBlockProps) {
  const { t } = useTranslation('common')
  const [expanded, setExpanded] = useState(false)

  // Auto-expand when streaming starts, keep user's choice when done
  useEffect(() => {
    if (isStreaming) setExpanded(true)
  }, [isStreaming])

  if (!text) return null

  return (
    <div className="mb-3 rounded-lg border border-border bg-surface-tertiary/50 overflow-hidden">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex items-center gap-2 w-full px-3 py-2 text-xs text-text-muted hover:text-text-secondary transition-colors"
      >
        {/* Brain icon */}
        <svg className={`w-3.5 h-3.5 shrink-0 ${isStreaming ? 'text-amber-500' : 'text-text-muted'}`} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <path d="M12 5a3 3 0 1 0-5.997.125 4 4 0 0 0-2.526 5.77 4 4 0 0 0 .556 6.588A4 4 0 1 0 12 18Z" />
          <path d="M12 5a3 3 0 1 1 5.997.125 4 4 0 0 1 2.526 5.77 4 4 0 0 1-.556 6.588A4 4 0 1 1 12 18Z" />
          <path d="M15 13a4.5 4.5 0 0 1-3-4 4.5 4.5 0 0 1-3 4" />
          <path d="M17.599 6.5a3 3 0 0 0 .399-1.375" /><path d="M6.003 5.125A3 3 0 0 0 6.401 6.5" />
          <path d="M3.477 10.896a4 4 0 0 1 .585-.396" /><path d="M19.938 10.5a4 4 0 0 1 .585.396" />
          <path d="M6 18a4 4 0 0 1-1.967-.516" /><path d="M19.967 17.484A4 4 0 0 1 18 18" />
        </svg>
        <span>{isStreaming ? t('thinkingStreaming') : t('thinking')}</span>
        {isStreaming && (
          <span className="inline-block w-1.5 h-3.5 bg-text-muted/50 animate-pulse rounded-sm" />
        )}
        <svg
          className={`w-3 h-3 ml-auto transition-transform ${expanded ? 'rotate-90' : ''}`}
          fill="none" viewBox="0 0 24 24" stroke="currentColor"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
        </svg>
      </button>

      {expanded && (
        <div className="px-3 pb-3">
          <pre className="text-xs text-text-secondary whitespace-pre-wrap leading-relaxed font-mono max-h-80 overflow-y-auto break-words">
            {text}
            {isStreaming && (
              <span className="inline-block w-1.5 h-3.5 bg-text-muted/50 animate-pulse rounded-sm ml-0.5 align-text-bottom" />
            )}
          </pre>
        </div>
      )}
    </div>
  )
}

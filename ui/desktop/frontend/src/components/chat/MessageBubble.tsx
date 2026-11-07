import { useTranslation } from 'react-i18next'
import type { ChatMessage as Message } from '../../stores/chat-store'
import { formatTimestamp } from '../../lib/format'
import { ThinkingBlock } from './ThinkingBlock'
import { ToolCallBlock } from './ToolCallBlock'
import { MarkdownRenderer } from './MarkdownRenderer'
import { MediaBlock } from './MediaBlock'

interface MessageBubbleProps {
  message: Message
  /** True when this is the last assistant message during an active run */
  isStreaming?: boolean
}

/** User messages are a right-aligned pill; agent replies are flat text with tool rows above. */
export function MessageBubble({ message, isStreaming }: MessageBubbleProps) {
  const { t } = useTranslation('chat')

  if (message.role === 'user') {
    return (
      <div className="flex justify-end">
        <p
          title={formatTimestamp(message.timestamp)}
          className="max-w-[70%] whitespace-pre-wrap break-words rounded-lg bg-bubble-user px-3.5 py-2 text-body text-ink"
        >
          {message.content}
        </p>
      </div>
    )
  }

  const outputTokens = message.usage?.outputTokens ?? 0
  const toolCalls = message.toolCalls ?? []
  const waiting = isStreaming && !message.content && !message.thinkingText && toolCalls.length === 0

  // A run that has not produced anything yet is shown by the activity line, not an empty block.
  if (waiting) return null

  return (
    <div className="flex min-w-0 flex-col gap-2">
      {message.thinkingText && <ThinkingBlock text={message.thinkingText} isStreaming={isStreaming && !message.content} />}

      {toolCalls.length > 0 && (
        <div className="flex flex-col gap-1.5">
          {toolCalls.map((tc) => <ToolCallBlock key={tc.toolId} toolCall={tc} />)}
        </div>
      )}

      {message.content && (
        <div className="min-w-0 overflow-hidden text-body text-ink-secondary">
          <MarkdownRenderer content={message.content} />
          {isStreaming && <span className="ml-0.5 inline-block h-4 w-0.5 animate-pulse rounded-sm bg-accent align-text-bottom" />}
        </div>
      )}

      {message.media?.length ? <MediaBlock items={message.media} /> : null}

      {!isStreaming && (outputTokens > 0 || message.model || message.provider) && (
        <div className="flex items-center gap-2 text-caption text-ink-muted">
          <time>{formatTimestamp(message.timestamp)}</time>
          {outputTokens > 0 && <span>· {t('thread.tokens', { count: outputTokens })}</span>}
          {(message.model || message.provider) && (
            <span className="truncate font-mono" title={message.provider}>· {message.model ?? message.provider}</span>
          )}
        </div>
      )}
    </div>
  )
}

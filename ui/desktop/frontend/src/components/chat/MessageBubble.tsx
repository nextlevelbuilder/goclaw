import type { Message } from '../../stores/chat-store'
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

export function MessageBubble({ message, isStreaming }: MessageBubbleProps) {
  if (message.role === 'user') {
    return (
      <div className="flex justify-end mb-4">
        <div className="max-w-[85%] bg-surface-secondary border border-border shadow-sm border-r-2 border-r-accent rounded-xl px-4 py-3">
          <p className="text-sm text-text-primary whitespace-pre-wrap break-words">{message.content}</p>
          <time className="text-[10px] text-text-muted mt-1 block text-right">
            {formatTimestamp(message.timestamp)}
          </time>
        </div>
      </div>
    )
  }

  // Assistant message
  const outputTokens = message.usage?.outputTokens ?? 0
  const toolCalls = message.toolCalls ?? []
  const hasTools = toolCalls.length > 0

  return (
    <div className="mb-6 min-w-0">
      {message.thinkingText && (
        <ThinkingBlock text={message.thinkingText} isStreaming={isStreaming && !message.content} />
      )}

      {/* Group tool calls in a single bordered container */}
      {hasTools && (
        <div className="mb-2 rounded-md border border-border bg-surface-tertiary/30 divide-y divide-border overflow-hidden">
          {toolCalls.map((tc) => (
            <ToolCallBlock key={tc.toolId} toolCall={tc} compact />
          ))}
        </div>
      )}

      {message.content && (
        <div className="min-w-0 overflow-hidden">
          <MarkdownRenderer content={message.content} />
          {/* Streaming cursor on last streaming message */}
          {isStreaming && (
            <span className="inline-block w-0.5 h-4 bg-accent animate-pulse rounded-sm ml-0.5 align-text-bottom" />
          )}
        </div>
      )}

      {/* Streaming cursor when no content yet but run is active */}
      {isStreaming && !message.content && !message.thinkingText && toolCalls.length === 0 && (
        <span className="inline-block w-0.5 h-4 bg-accent animate-pulse rounded-sm" />
      )}

      {message.media?.length ? <MediaBlock items={message.media} /> : null}

      <div className="flex items-center gap-2 mt-1.5 text-[10px] text-text-muted">
        <time>{formatTimestamp(message.timestamp)}</time>
        {outputTokens > 0 && (
          <span>· {outputTokens.toLocaleString()} tokens</span>
        )}
      </div>
    </div>
  )
}

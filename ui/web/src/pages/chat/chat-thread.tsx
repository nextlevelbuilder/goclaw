import { useTranslation } from "react-i18next";
import { Circle } from "lucide-react";
import { MessageBubble } from "@/components/chat/message-bubble";
import { StreamingText } from "@/components/chat/streaming-text";
import { ToolCallCard } from "@/components/chat/tool-call-card";
import { ThinkingIndicator } from "@/components/chat/thinking-indicator";
import { ThinkingBlock } from "@/components/chat/thinking-block";
import { useAutoScroll } from "@/hooks/use-auto-scroll";
import type { ChatMessage, ToolStreamEntry } from "@/types/chat";

interface ChatThreadProps {
  messages: ChatMessage[];
  streamText: string | null;
  thinkingText: string | null;
  toolStream: ToolStreamEntry[];
  isRunning: boolean;
  loading?: boolean;
  scrollTrigger?: number;
}

export function ChatThread({
  messages,
  streamText,
  thinkingText,
  toolStream,
  isRunning,
  loading,
  scrollTrigger = 0,
}: ChatThreadProps) {
  const { t } = useTranslation("chat");
  const { ref, onScroll } = useAutoScroll<HTMLDivElement>(
    [messages.length, streamText, thinkingText, toolStream.length],
    100,
    scrollTrigger,
  );

  // Show spinner while loading history for a different session
  if (loading) {
    return (
      <div className="flex flex-1 items-center justify-center">
        <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
      </div>
    );
  }

  if (messages.length === 0 && !isRunning) {
    return (
      <div className="flex flex-1 flex-col items-center justify-center gap-2 text-muted-foreground">
        <p className="text-lg font-medium">{t("empty.title")}</p>
        <p className="text-sm">{t("empty.description")}</p>
      </div>
    );
  }

  return (
    <div
      ref={ref}
      onScroll={onScroll}
      className="flex-1 overflow-y-auto overscroll-contain px-4 py-4"
    >
      <div className="mx-auto max-w-3xl space-y-4">
        {messages
          .filter((msg) => !(msg.role === "user" && typeof msg.content === "string" && msg.content.startsWith("[System]")))
          .map((msg, i) => (
            <MessageBubble key={`${msg.role}-${i}`} message={msg} />
          ))}

        {/* Tool stream during active run */}
        {toolStream.length > 0 && (
          <div className="space-y-1">
            {toolStream.map((entry) => (
              <ToolCallCard key={entry.toolCallId} entry={entry} />
            ))}
          </div>
        )}

        {/* Thinking block (extended thinking / reasoning) */}
        {isRunning && thinkingText && (
          <ThinkingBlock text={thinkingText} isStreaming={streamText === null} />
        )}

        {/* Streaming text */}
        {isRunning && streamText !== null && (
          <div className="flex gap-3">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full border bg-background">
              <Circle className="h-4 w-4" />
            </div>
            <div className="max-w-[80%] rounded-lg bg-muted px-4 py-2">
              <StreamingText text={streamText} />
            </div>
          </div>
        )}

        {/* Thinking indicator when running but no stream yet */}
        {isRunning && streamText === null && !thinkingText && toolStream.length === 0 && (
          <div className="flex gap-3">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full border bg-background">
              <Circle className="h-4 w-4" />
            </div>
            <div className="rounded-lg bg-muted px-4 py-2">
              <ThinkingIndicator />
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

import { Bot, User } from "lucide-react";
import { MessageContent } from "./message-content";
import { ThinkingBlock } from "./thinking-block";
import { ToolCallCard } from "./tool-call-card";
import { BlockReplyBubble } from "./block-reply-bubble";
import { MediaGallery } from "./media-gallery";
import { useUiStore } from "@/stores/use-ui-store";
import { resolveTimezone } from "@/lib/format";
import type { ChatMessage } from "@/types/chat";

interface MessageBubbleProps {
  message: ChatMessage;
}

export function MessageBubble({ message }: MessageBubbleProps) {
  const timezone = useUiStore((s) => s.timezone);
  const isUser = message.role === "user";
  const isTool = message.role === "tool";

  if (isTool) return null;
  if (message.isNotification) return null;
  if (message.isBlockReply) return <BlockReplyBubble message={message} />;

  const isAssistant = message.role === "assistant";
  const hasThinking = isAssistant && !!message.thinking;
  const hasToolDetails = isAssistant && message.toolDetails && message.toolDetails.length > 0;
  const hasToolCalls = isAssistant && message.tool_calls && message.tool_calls.length > 0;
  const hasContent = !!message.content?.trim();

  if (isAssistant && !hasContent && !hasToolCalls && !hasToolDetails) return null;

  // Tool-only message (no text content) — render compact without bubble wrapper
  const isToolOnly = isAssistant && !hasContent && !hasThinking && (hasToolDetails || hasToolCalls);

  return (
    <div className={`flex gap-3 ${isUser ? "flex-row-reverse" : ""}`}>
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full border bg-background">
        {isUser ? <User className="h-4 w-4" /> : <Bot className="h-4 w-4" />}
      </div>

      {isToolOnly ? (
        /* Compact tool-only card — no bubble wrapper, full width */
        <div className="flex-1 min-w-0 rounded-md border bg-muted divide-y divide-border">
          {hasThinking && (
            <div className="px-2 py-1.5">
              <ThinkingBlock text={message.thinking!} />
            </div>
          )}
          {hasToolDetails && message.toolDetails!.map((entry) => (
            <ToolCallCard key={entry.toolCallId} entry={entry} compact />
          ))}
        </div>
      ) : (
        /* Normal message bubble — assistant uses full width, user capped at 85% */
        <div className={`rounded-lg px-4 py-2 ${
          isUser
            ? "max-w-[85%] bg-card text-card-foreground border border-border shadow-sm border-r-2 border-r-accent-foreground"
            : "flex-1 min-w-0 bg-card text-card-foreground border border-border shadow-sm"
        }`}>
          {hasThinking && (
            <div className="mb-2">
              <ThinkingBlock text={message.thinking!} />
            </div>
          )}
          {hasToolDetails && (
            <div className="mb-2 rounded-md border bg-muted divide-y divide-border">
              {message.toolDetails!.map((entry) => (
                <ToolCallCard key={entry.toolCallId} entry={entry} compact />
              ))}
            </div>
          )}
          <MessageContent content={message.content} role={message.role} mediaBasenames={message.mediaItems?.map((m) => m.path.split("/").pop() ?? "").filter(Boolean)} />
          {message.mediaItems && message.mediaItems.length > 0 && (
            <div className="mt-2">
              <MediaGallery items={message.mediaItems} />
            </div>
          )}
          {message.timestamp && (
            <div className="mt-1 text-[10px] text-muted-foreground">
              {new Intl.DateTimeFormat([], {
                timeZone: resolveTimezone(timezone),
                hour: "numeric",
                minute: "2-digit",
              }).format(new Date(message.timestamp))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

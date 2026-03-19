import { Bot, User } from "lucide-react";
import { MessageContent } from "./message-content";
import { ThinkingBlock } from "./thinking-block";
import { ToolCallCard } from "./tool-call-card";
import type { ChatMessage } from "@/types/chat";

interface MessageBubbleProps {
  message: ChatMessage;
}

export function MessageBubble({ message }: MessageBubbleProps) {
  const isUser = message.role === "user";
  const isTool = message.role === "tool";
  const isCompaction = message.role === "system";

  if (isTool) {
    return null; // Tool messages are shown inline with assistant messages
  }

  if (isCompaction) {
    return (
      <div className="flex items-center gap-3 py-1">
        <div className="h-px flex-1 bg-border" />
        <span className="text-[11px] text-muted-foreground px-2 shrink-0">{message.content}</span>
        <div className="h-px flex-1 bg-border" />
      </div>
    );
  }

  const isAssistant = message.role === "assistant";
  const hasThinking = isAssistant && !!message.thinking;
  const hasToolDetails = isAssistant && message.toolDetails && message.toolDetails.length > 0;
  const hasToolCalls = isAssistant && message.tool_calls && message.tool_calls.length > 0;
  const hasContent = !!message.content?.trim();

  // Skip assistant messages with no content at all
  if (isAssistant && !hasContent && !hasToolCalls && !hasToolDetails) {
    return null;
  }

  return (
    <div className={`flex gap-3 ${isUser ? "flex-row-reverse" : ""}`}>
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full border bg-background">
        {isUser ? (
          <User className="h-4 w-4" />
        ) : (
          <Bot className="h-4 w-4" />
        )}
      </div>

      <div
        className={`max-w-[80%] rounded-lg px-4 py-2 ${
          isUser
            ? "bg-primary text-primary-foreground"
            : "bg-card text-card-foreground border border-border shadow-sm"
        }`}
      >
        {hasThinking && (
          <div className="mb-2">
            <ThinkingBlock text={message.thinking!} />
          </div>
        )}
        {hasToolDetails && (
          <div className="mb-2">
            {message.toolDetails!.map((entry) => (
              <ToolCallCard key={entry.toolCallId} entry={entry} />
            ))}
          </div>
        )}
        <MessageContent content={message.content} role={message.role} />
        {message.timestamp && (
          <div className={`mt-1 text-[10px] ${isUser ? "text-primary-foreground/60" : "text-muted-foreground"}`}>
            {new Date(message.timestamp).toLocaleTimeString([], {
              hour: "numeric",
              minute: "2-digit",
            })}
          </div>
        )}
      </div>
    </div>
  );
}

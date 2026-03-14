import { Bot, User } from "lucide-react";
import { MessageContent } from "./message-content";
import { ThinkingBlock } from "./thinking-block";
import type { ChatMessage } from "@/types/chat";

interface MessageBubbleProps {
  message: ChatMessage;
  /** Hide the avatar icon for consecutive same-role messages */
  hideAvatar?: boolean;
}

export function MessageBubble({ message, hideAvatar }: MessageBubbleProps) {
  const isUser = message.role === "user";
  const isTool = message.role === "tool";

  if (isTool) {
    return null;
  }

  const isAssistant = message.role === "assistant";
  const hasThinking = isAssistant && !!message.thinking;
  const hasContent = !!message.content?.trim();

  if (isAssistant && !hasContent && !hasThinking) {
    return null;
  }

  return (
    <div className={`flex gap-3 ${isUser ? "flex-row-reverse" : ""}`}>
      {hideAvatar ? (
        <div className="w-8 shrink-0" />
      ) : (
        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full border bg-background">
          {isUser ? (
            <User className="h-4 w-4" />
          ) : (
            <Bot className="h-4 w-4" />
          )}
        </div>
      )}

      <div
        className={`max-w-[80%] rounded-lg px-4 py-2 ${isUser
          ? "bg-primary text-primary-foreground"
          : "bg-white dark:bg-card text-card-foreground border border-border shadow-sm"
          }`}
      >
        {hasThinking && (
          <div className="mb-2">
            <ThinkingBlock text={message.thinking!} />
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

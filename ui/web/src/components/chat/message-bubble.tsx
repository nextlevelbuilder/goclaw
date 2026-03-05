import { Bot, User } from "lucide-react";
import { MessageContent } from "./message-content";
import type { ChatMessage } from "@/types/chat";

interface MessageBubbleProps {
  message: ChatMessage;
}

export function MessageBubble({ message }: MessageBubbleProps) {
  const isUser = message.role === "user";
  const isTool = message.role === "tool";

  if (isTool) {
    return null; // Tool messages are shown inline with assistant messages
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

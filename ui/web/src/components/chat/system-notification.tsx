import type { ChatMessage } from "@/types/chat";

interface SystemNotificationProps {
  message: ChatMessage;
}

export function SystemNotification({ message }: SystemNotificationProps) {
  return (
    <div className="flex items-center gap-3 py-2 text-xs text-muted-foreground">
      <div className="h-px flex-1 bg-border" />
      <span className="shrink-0 whitespace-nowrap">{message.content}</span>
      <div className="h-px flex-1 bg-border" />
    </div>
  );
}

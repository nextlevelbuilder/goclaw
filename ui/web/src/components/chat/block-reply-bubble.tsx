import { MarkdownRenderer } from "@/components/shared/markdown-renderer";
import type { ChatMessage } from "@/types/chat";

interface BlockReplyBubbleProps {
  message: ChatMessage;
}

export function BlockReplyBubble({ message }: BlockReplyBubbleProps) {
  return (
    <div className="border-l-2 border-muted-foreground/30 pl-3 py-1">
      <div className="text-sm text-muted-foreground italic">
        <MarkdownRenderer content={message.content} />
      </div>
    </div>
  );
}

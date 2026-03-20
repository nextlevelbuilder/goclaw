import { Bot } from "lucide-react";
import { ActivityIndicator } from "./activity-indicator";
import { BlockReplyBubble } from "./block-reply-bubble";
import { ThinkingBlock } from "./thinking-block";
import { StreamingText } from "./streaming-text";
import { ToolCallCard } from "./tool-call-card";
import type { RunActivity, ToolStreamEntry, ChatMessage } from "@/types/chat";

interface ActiveRunZoneProps {
  isRunning: boolean;
  activity: RunActivity | null;
  thinkingText: string | null;
  streamText: string | null;
  toolStream: ToolStreamEntry[];
  blockReplies: ChatMessage[];
}

export function ActiveRunZone({
  isRunning,
  activity,
  thinkingText,
  streamText,
  toolStream,
  blockReplies,
}: ActiveRunZoneProps) {
  const hasContent =
    blockReplies.length > 0 ||
    toolStream.length > 0 ||
    thinkingText !== null ||
    streamText !== null;

  if (!isRunning && !hasContent) return null;

  return (
    <div className="flex gap-3">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full border bg-background">
        <Bot className="h-4 w-4" />
      </div>

      <div className="flex-1 space-y-3">
        {blockReplies.map((msg, i) => (
          <BlockReplyBubble key={msg.timestamp ?? i} message={msg} />
        ))}

        {toolStream.map((entry) => (
          <ToolCallCard key={entry.toolCallId} entry={entry} />
        ))}

        {thinkingText !== null && (
          <ThinkingBlock text={thinkingText} isStreaming={isRunning && streamText === null} />
        )}

        {streamText !== null && <StreamingText text={streamText} />}

        {isRunning && (
          <ActivityIndicator activity={activity} isRunning={isRunning} />
        )}
      </div>
    </div>
  );
}

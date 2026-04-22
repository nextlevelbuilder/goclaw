import { memo } from "react";
import { useTranslation } from "react-i18next";
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
  /**
   * When true, a blurred image-gen skeleton placeholder is shown in the
   * streaming bubble. Set by ChatThread when the current agent uses a
   * provider that supports native image generation and the toggle is on.
   */
  showImageGenPlaceholder?: boolean;
}

export const ActiveRunZone = memo(function ActiveRunZone({
  isRunning,
  activity,
  thinkingText,
  streamText,
  toolStream,
  blockReplies,
  showImageGenPlaceholder = false,
}: ActiveRunZoneProps) {
  const { t } = useTranslation("chat");
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

      <div className="flex-1 min-w-0 space-y-3">
        {blockReplies.map((msg, i) => (
          <BlockReplyBubble key={msg.timestamp ?? i} message={msg} />
        ))}

        {/* Tool cards: match MessageBubble's compact grouped layout */}
        {toolStream.length > 0 && (
          <div className="rounded-md border bg-muted divide-y divide-border">
            {toolStream.map((entry) => (
              <ToolCallCard key={entry.toolCallId} entry={entry} compact />
            ))}
          </div>
        )}

        {/* Streaming text: wrap in bubble matching MessageBubble's assistant style */}
        {(thinkingText !== null || streamText !== null || (isRunning && showImageGenPlaceholder)) && (
          <div className="flex-1 min-w-0 rounded-lg px-4 py-2 bg-card text-card-foreground border border-border shadow-sm">
            {thinkingText !== null && (
              <div className={streamText !== null ? "mb-2" : ""}>
                <ThinkingBlock text={thinkingText} isStreaming={isRunning && streamText === null} />
              </div>
            )}
            {streamText !== null && <StreamingText text={streamText} />}
            {/* Image-gen skeleton: visible from run start until run.completed delivers the image */}
            {isRunning && showImageGenPlaceholder && (
              <div className="mt-2 flex flex-col gap-1.5">
                <div className="h-40 w-full max-w-[240px] rounded-lg bg-muted animate-pulse overflow-hidden relative">
                  <div className="absolute inset-0 flex items-center justify-center">
                    <div className="h-5 w-5 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
                  </div>
                </div>
                <span className="text-xs text-muted-foreground">{t("imageGenGenerating")}</span>
              </div>
            )}
          </div>
        )}

        {(isRunning || activity?.phase === "leader_processing") && (
          <ActivityIndicator activity={activity} isRunning={isRunning} />
        )}
      </div>
    </div>
  );
});

import { useTranslation } from "react-i18next";
import { Bot, Circle } from "lucide-react";
import { MessageBubble } from "@/components/chat/message-bubble";
import { StreamingText } from "@/components/chat/streaming-text";
import { ToolCallCard } from "@/components/chat/tool-call-card";
import { ThinkingIndicator } from "@/components/chat/thinking-indicator";
import { ThinkingBlock } from "@/components/chat/thinking-block";
import { useAutoScroll } from "@/hooks/use-auto-scroll";
import type { ChatMessage, ToolStreamEntry } from "@/types/chat";

interface ChatThreadProps {
  messages: ChatMessage[];
  summary?: string | null;
  streamText: string | null;
  thinkingText: string | null;
  toolStream: ToolStreamEntry[];
  isRunning: boolean;
  loading?: boolean;
  scrollTrigger?: number;
}

/**
 * Group consecutive assistant messages into a single visual turn.
 * User messages and role switches create new groups.
 * Tool messages (role=tool) are absorbed into the preceding assistant group.
 */
interface MessageGroup {
  role: "user" | "assistant";
  messages: ChatMessage[];
}

function groupMessages(messages: ChatMessage[]): MessageGroup[] {
  const groups: MessageGroup[] = [];
  for (const msg of messages) {
    if (msg.role === "tool") continue;
    const last = groups[groups.length - 1];
    if (last && last.role === msg.role) {
      last.messages.push(msg);
    } else {
      groups.push({ role: msg.role as "user" | "assistant", messages: [msg] });
    }
  }
  return groups;
}

/**
 * Render an assistant group: collect all tool details into one compact block,
 * then render text-content messages below.
 */
function AssistantGroup({ group }: { group: MessageGroup }) {
  // Collect all tool details from every message in the group
  const allTools: ToolStreamEntry[] = [];
  const textMessages: ChatMessage[] = [];

  for (const msg of group.messages) {
    if (msg.toolDetails && msg.toolDetails.length > 0) {
      allTools.push(...msg.toolDetails);
    }
    if (msg.content?.trim()) {
      textMessages.push(msg);
    }
  }

  return (
    <div className="space-y-3">
      {/* Single merged tool block */}
      {allTools.length > 0 && (
        <div className="flex gap-3">
          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full border bg-background">
            <Bot className="h-4 w-4" />
          </div>
          <div className="max-w-[80%] space-y-1">
            {allTools.map((entry) => (
              <ToolCallCard key={entry.toolCallId} entry={entry} />
            ))}
          </div>
        </div>
      )}

      {/* Text content messages */}
      {textMessages.map((msg, i) => (
        <MessageBubble
          key={i}
          message={msg}
          hideAvatar={allTools.length > 0 || i > 0}
        />
      ))}
    </div>
  );
}

export function ChatThread({
  messages,
  summary,
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

  const groups = groupMessages(messages);

  return (
    <div
      ref={ref}
      onScroll={onScroll}
      className="flex-1 overflow-y-auto overscroll-contain px-4 py-4"
    >
      <div className="mx-auto max-w-3xl space-y-4">
        {/* Summary of compacted earlier conversation */}
        {summary && (
          <div className="rounded-lg border border-dashed border-muted-foreground/30 bg-muted/30 px-4 py-3 text-sm text-muted-foreground">
            <div className="mb-1 text-xs font-medium uppercase tracking-wide">{t("conversationSummary")}</div>
            <p className="whitespace-pre-wrap">{summary}</p>
          </div>
        )}

        {groups.map((group, gi) =>
          group.role === "assistant" ? (
            <AssistantGroup key={gi} group={group} />
          ) : (
            <div key={gi}>
              {group.messages.map((msg, mi) => (
                <MessageBubble
                  key={`${gi}-${mi}`}
                  message={msg}
                  hideAvatar={mi > 0}
                />
              ))}
            </div>
          ),
        )}

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
            <div className="max-w-[80%] rounded-lg bg-white dark:bg-muted border border-border shadow-sm px-4 py-2">
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

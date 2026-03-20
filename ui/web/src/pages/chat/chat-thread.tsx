import { memo, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { Bot } from "lucide-react";
import { MessageBubble } from "@/components/chat/message-bubble";
import { ActiveRunZone } from "@/components/chat/active-run-zone";
import { SystemNotification } from "@/components/chat/system-notification";
import { TeamActivityPanel } from "@/components/chat/team-activity-panel";
import { ToolCallCard } from "@/components/chat/tool-call-card";
import { ThinkingBlock } from "@/components/chat/thinking-block";
import { useAutoScroll } from "@/hooks/use-auto-scroll";
import type { ChatMessage, ToolStreamEntry, RunActivity, ActiveTeamTask } from "@/types/chat";

interface ChatThreadProps {
  messages: ChatMessage[];
  streamText: string | null;
  thinkingText: string | null;
  toolStream: ToolStreamEntry[];
  blockReplies: ChatMessage[];
  activity: RunActivity | null;
  teamTasks: ActiveTeamTask[];
  isRunning: boolean;
  loading?: boolean;
  scrollTrigger?: number;
}

/** Check if a message is tool-only (no user-visible text content) */
function isToolOnlyMsg(msg: ChatMessage): boolean {
  if (msg.role !== "assistant") return false;
  const hasContent = !!msg.content?.trim();
  const hasTools = (msg.toolDetails && msg.toolDetails.length > 0) || (msg.tool_calls && msg.tool_calls.length > 0);
  return !hasContent && !!hasTools;
}

type DisplayItem =
  | { kind: "message"; msg: ChatMessage; idx: number }
  | { kind: "notification"; msg: ChatMessage; idx: number }
  | { kind: "merged-tools"; msgs: ChatMessage[]; idx: number };

/** Merge consecutive tool-only assistant messages into single groups */
function buildDisplayItems(messages: ChatMessage[]): DisplayItem[] {
  const filtered = messages.filter(
    (msg) => !(msg.role === "user" && typeof msg.content === "string" && msg.content.startsWith("[System]")),
  );

  const items: DisplayItem[] = [];
  let toolGroup: ChatMessage[] = [];
  let groupStartIdx = 0;

  const flushToolGroup = () => {
    if (toolGroup.length > 0) {
      items.push({ kind: "merged-tools", msgs: toolGroup, idx: groupStartIdx });
      toolGroup = [];
    }
  };

  filtered.forEach((msg, i) => {
    if (msg.isNotification) {
      flushToolGroup();
      items.push({ kind: "notification", msg, idx: i });
    } else if (isToolOnlyMsg(msg)) {
      if (toolGroup.length === 0) groupStartIdx = i;
      toolGroup.push(msg);
    } else {
      flushToolGroup();
      items.push({ kind: "message", msg, idx: i });
    }
  });
  flushToolGroup();

  return items;
}

export const ChatThread = memo(function ChatThread({
  messages, streamText, thinkingText, toolStream, blockReplies,
  activity, teamTasks, isRunning, loading, scrollTrigger = 0,
}: ChatThreadProps) {
  const { t } = useTranslation("chat");
  const { ref, onScroll } = useAutoScroll<HTMLDivElement>(
    [messages.length, streamText, thinkingText, toolStream.length],
    100,
    scrollTrigger,
  );

  const displayItems = useMemo(() => buildDisplayItems(messages), [messages]);

  if (messages.length === 0 && !isRunning) {
    if (loading) {
      return (
        <div className="flex flex-1 items-center justify-center">
          <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
        </div>
      );
    }
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
      style={{
        backgroundImage: "radial-gradient(circle, var(--color-border) 1px, transparent 1px)",
        backgroundSize: "24px 24px",
      }}
    >
      <div className="mx-auto max-w-3xl space-y-3">
        {displayItems.map((item) => {
          switch (item.kind) {
            case "notification":
              return <SystemNotification key={`notif-${item.idx}`} message={item.msg} />;
            case "message":
              return <MessageBubble key={`msg-${item.idx}`} message={item.msg} />;
            case "merged-tools":
              return <MergedToolGroup key={`tools-${item.idx}`} msgs={item.msgs} />;
          }
        })}

        {teamTasks.length > 0 && <TeamActivityPanel tasks={teamTasks} />}

        <ActiveRunZone
          isRunning={isRunning}
          activity={activity}
          thinkingText={thinkingText}
          streamText={streamText}
          toolStream={toolStream}
          blockReplies={blockReplies}
        />
      </div>
    </div>
  );
});

/** Renders multiple consecutive tool-only messages as a single compact card */
function MergedToolGroup({ msgs }: { msgs: ChatMessage[] }) {
  // Collect all tool details from all messages
  const allTools = msgs.flatMap((msg) => msg.toolDetails ?? []);
  const allThinking = msgs.map((m) => m.thinking).filter(Boolean);

  return (
    <div className="flex gap-3">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full border bg-background">
        <Bot className="h-4 w-4" />
      </div>
      <div className="flex-1 min-w-0 rounded-md border bg-muted/30 divide-y divide-border">
        {allThinking.length > 0 && (
          <div className="px-2 py-1.5">
            <ThinkingBlock text={allThinking.join("\n\n")} />
          </div>
        )}
        {allTools.map((entry) => (
          <ToolCallCard key={entry.toolCallId} entry={entry} compact />
        ))}
      </div>
    </div>
  );
}

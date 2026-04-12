import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import { ArrowLeft, Trash2, RotateCcw, Eye, Pencil, Check, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { MessageBubble } from "@/components/chat/message-bubble";
import { ConfirmDialog } from "@/components/shared/confirm-dialog";
import { useWsEvent } from "@/hooks/use-ws-event";
import { useDebouncedCallback } from "@/hooks/use-debounced-callback";
import { Events } from "@/api/protocol";
import { parseSessionKey } from "@/lib/session-key";
import { formatDate, formatTokens } from "@/lib/format";
import type { SessionInfo, SessionPreview, Message } from "@/types/session";
import type { ChatMessage, AgentEventPayload, ToolStreamEntry } from "@/types/chat";
import { messageToTimestamp } from "@/lib/message-utils";
import { SystemMessageBlock, SummaryBlock } from "./session-message-blocks";

/** Check if a message is an internal system message (subagent results, cron, etc.) */
function isSystemMessage(msg: ChatMessage): boolean {
  const c = msg.content?.trimStart() ?? "";
  return c.startsWith("[System Message]") || c.startsWith("[System]");
}

/** Check if a message should be displayed */
function isDisplayable(msg: ChatMessage): boolean {
  // Hide tool role messages (shown inline with assistant)
  if (msg.role === "tool") return false;
  // Show if there is text content
  if (msg.content?.trim()) return true;
  // Assistant messages with tool calls or thinking should still be displayed
  if (msg.role === "assistant") {
    return !!(msg.toolDetails?.length || msg.tool_calls?.length || msg.thinking?.trim());
  }
  return false;
}

interface SessionDetailPageProps {
  session: SessionInfo;
  onBack: () => void;
  onPreview: (key: string) => Promise<SessionPreview | null>;
  onDelete: (key: string) => Promise<void>;
  onReset: (key: string) => Promise<void>;
  onPatch?: (key: string, updates: { label?: string }) => Promise<void>;
}

export function SessionDetailPage({
  session,
  onBack,
  onPreview,
  onDelete,
  onReset,
  onPatch,
}: SessionDetailPageProps) {
  const { t } = useTranslation("sessions");
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [summary, setSummary] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [confirmReset, setConfirmReset] = useState(false);
  const [editingTitle, setEditingTitle] = useState(false);
  const [titleDraft, setTitleDraft] = useState("");

  const parsed = parseSessionKey(session.key);

  const loadMessages = useCallback(() => {
    onPreview(session.key)
      .then((preview) => {
        if (preview) {
          const allMsgs = preview.messages;
          // Build a map of tool_call_id -> tool message for result lookup
          const toolResultMap = new Map<string, Message>();
          for (const m of allMsgs) {
            if (m.role === "tool" && m.tool_call_id) {
              toolResultMap.set(m.tool_call_id, m);
            }
          }
          setMessages(
            allMsgs.map((m, i) => {
              const chatMsg: ChatMessage = {
                ...m,
                timestamp: messageToTimestamp(m, i, allMsgs.length),
              };
              // Reconstruct toolDetails for assistant messages with tool_calls
              if (m.role === "assistant" && m.tool_calls && m.tool_calls.length > 0) {
                chatMsg.toolDetails = m.tool_calls.map((tc) => {
                  const toolMsg = toolResultMap.get(tc.id);
                  return {
                    toolCallId: tc.id,
                    runId: "",
                    name: tc.name,
                    phase: (toolMsg ? "completed" : "calling") as ToolStreamEntry["phase"],
                    startedAt: 0,
                    updatedAt: 0,
                    arguments: tc.arguments,
                    result: toolMsg?.content,
                  };
                });
              }
              return chatMsg;
            }),
          );
          setSummary(preview.summary ?? null);
        }
      })
      .finally(() => setLoading(false));
  }, [session.key, onPreview]);

  useEffect(() => {
    setLoading(true);
    loadMessages();
  }, [loadMessages]);

  // Auto-refresh when the agent for this session completes a run
  const debouncedRefresh = useDebouncedCallback(loadMessages, 2000);

  const handleAgentEvent = useCallback(
    (payload: unknown) => {
      const event = payload as AgentEventPayload;
      if (!event) return;
      if (
        (event.type === "run.completed" || event.type === "run.failed" || event.type === "run.cancelled") &&
        event.agentId === parsed.agentId
      ) {
        debouncedRefresh();
      }
    },
    [debouncedRefresh, parsed.agentId],
  );

  useWsEvent(Events.AGENT, handleAgentEvent);

  return (
    <div className="flex h-full flex-col">
      {/* Header */}
      <div className="flex items-center justify-between border-b p-4">
        <div className="flex items-center gap-3">
          <Button variant="ghost" size="icon" onClick={onBack}>
            <ArrowLeft className="h-4 w-4" />
          </Button>
          <div>
            {editingTitle ? (
              <div className="flex items-center gap-1">
                <input
                  autoFocus
                  className="h-7 rounded border bg-background px-2 text-base md:text-sm font-medium outline-none focus:ring-1 focus:ring-ring"
                  value={titleDraft}
                  onChange={(e) => setTitleDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      onPatch?.(session.key, { label: titleDraft });
                      setEditingTitle(false);
                    } else if (e.key === "Escape") {
                      setEditingTitle(false);
                    }
                  }}
                />
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-6 w-6"
                  onClick={() => {
                    onPatch?.(session.key, { label: titleDraft });
                    setEditingTitle(false);
                  }}
                >
                  <Check className="h-3.5 w-3.5" />
                </Button>
                <Button variant="ghost" size="icon" className="h-6 w-6" onClick={() => setEditingTitle(false)}>
                  <X className="h-3.5 w-3.5" />
                </Button>
              </div>
            ) : (
              <h3
                className="group flex cursor-pointer items-center gap-1.5 font-medium hover:text-primary"
                onClick={() => {
                  setTitleDraft(session.label || session.metadata?.chat_title || session.metadata?.display_name || "");
                  setEditingTitle(true);
                }}
              >
                {session.metadata?.chat_title || session.metadata?.display_name || session.label || parsed.scope}
                <Pencil className="h-3 w-3 opacity-0 transition-opacity group-hover:opacity-50" />
              </h3>
            )}
            <div className="mt-1 flex items-center gap-2 text-xs text-muted-foreground">
              <Badge variant="outline">{session.agentName || parsed.agentId}</Badge>
              {session.channel && session.channel !== "ws" && (
                <Badge variant="secondary" className="gap-1">
                  <Eye className="h-3 w-3" />
                  {session.channel}
                </Badge>
              )}
              {session.metadata?.username && (
                <Badge variant="secondary">@{session.metadata.username}</Badge>
              )}
              {session.metadata?.peer_kind && (
                <Badge variant="outline">{session.metadata.peer_kind}</Badge>
              )}
              <span>{session.messageCount} {t("detail.messages")}</span>
              <span>{formatDate(session.updated)}</span>
              {session.inputTokens != null && (
                <span>
                  {formatTokens(session.inputTokens)} in / {formatTokens(session.outputTokens ?? 0)} out
                </span>
              )}
            </div>
          </div>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={() => setConfirmReset(true)} className="gap-1">
            <RotateCcw className="h-3.5 w-3.5" /> {t("detail.reset")}
          </Button>
          <Button variant="destructive" size="sm" onClick={() => setConfirmDelete(true)} className="gap-1">
            <Trash2 className="h-3.5 w-3.5" /> {t("detail.delete")}
          </Button>
        </div>
      </div>

      {/* Summary */}
      {summary && (
        <SummaryBlock text={summary} />
      )}

      {/* Messages */}
      <div className="flex-1 overflow-y-auto px-6 py-4">
        {loading && messages.length === 0 ? (
          <div className="flex items-center justify-center py-12">
            <div className="h-6 w-6 animate-spin rounded-full border-2 border-muted-foreground border-t-transparent" />
          </div>
        ) : messages.length === 0 ? (
          <div className="py-12 text-center text-sm text-muted-foreground">
            {t("detail.noMessages")}
          </div>
        ) : (
          <div className="mx-auto max-w-3xl space-y-4">
            {messages.filter(isDisplayable).map((msg, i) =>
              isSystemMessage(msg) ? (
                <SystemMessageBlock key={i} content={msg.content} />
              ) : (
                <MessageBubble key={i} message={msg} />
              ),
            )}
          </div>
        )}
      </div>

      <ConfirmDialog
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        title={t("detail.deleteTitle")}
        description={t("detail.deleteDescription")}
        confirmLabel={t("detail.confirmDelete")}
        variant="destructive"
        onConfirm={async () => {
          await onDelete(session.key);
          setConfirmDelete(false);
        }}
      />

      <ConfirmDialog
        open={confirmReset}
        onOpenChange={setConfirmReset}
        title={t("detail.resetTitle")}
        description={t("detail.resetDescription")}
        confirmLabel={t("detail.confirmReset")}
        onConfirm={async () => {
          await onReset(session.key);
          setConfirmReset(false);
          setMessages([]);
        }}
      />
    </div>
  );
}


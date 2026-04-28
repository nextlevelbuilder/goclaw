import type { Message } from "@/types/session";
import type { ChatMessage, ToolStreamEntry, MediaItem } from "@/types/chat";
import { toFileUrl } from "@/lib/file-helpers";
import { messageToTimestamp } from "@/lib/message-utils";

/**
 * Transform raw Message[] from history RPC into ChatMessage[] for display.
 * Reconstructs toolDetails from tool_calls + tool result messages,
 * and converts media_refs to MediaItem[] for gallery.
 */
export function transformHistoryMessages(
  allMsgs: Message[],
  mediaItems?: MediaItem[],
): ChatMessage[] {
  // Build a map of tool_call_id -> tool message for result lookup
  const toolResultMap = new Map<string, Message>();
  for (const m of allMsgs) {
    if (m.role === "tool" && m.tool_call_id) {
      toolResultMap.set(m.tool_call_id, m);
    }
  }

  const msgs: ChatMessage[] = allMsgs.map((m: Message, i: number) => {
    const chatMsg: ChatMessage = {
      ...m,
      timestamp: messageToTimestamp(m, i, allMsgs.length),
    };

    // Convert persisted media_refs to mediaItems for gallery display
    if (m.role === "assistant" && m.media_refs && m.media_refs.length > 0) {
      chatMsg.mediaItems = m.media_refs.map((ref) => ({
        path: toFileUrl(ref.path || ref.id),
        mimeType: ref.mime_type,
        fileName: (ref.path?.split("?")[0]?.split("/").pop()) ?? ref.id,
        kind: (ref.kind as MediaItem["kind"]) || "document",
        prompt: ref.prompt || undefined,
      }));
    }

    // Reconstruct toolDetails for assistant messages with tool_calls
    if (m.role === "assistant" && m.tool_calls && m.tool_calls.length > 0) {
      chatMsg.toolDetails = m.tool_calls.map((tc) => {
        const toolMsg = toolResultMap.get(tc.id);
        return {
          toolCallId: tc.id,
          runId: "",
          name: tc.name,
          phase: (toolMsg ? (toolMsg.is_error ? "error" : "completed") : "calling") as ToolStreamEntry["phase"],
          startedAt: 0,
          updatedAt: 0,
          arguments: tc.arguments,
          result: toolMsg?.content,
        };
      });
    }

    return chatMsg;
  });

  // Attach media to the last assistant message if provided
  if (mediaItems?.length && msgs.length > 0) {
    for (let i = msgs.length - 1; i >= 0; i--) {
      if (msgs[i]!.role === "assistant") {
        msgs[i] = { ...msgs[i]!, mediaItems };
        break;
      }
    }
  }

  return msgs;
}

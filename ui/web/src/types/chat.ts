/** Chat-specific types for the chat page UI */

import type { Message } from "./session";

/** Activity phase tracking during agent run */
export interface RunActivity {
  phase: "thinking" | "tool_exec" | "streaming" | "compacting" | "retrying" | "leader_processing";
  tool?: string;
  tools?: string[];
  iteration?: number;
  retryAttempt?: number;
  retryMax?: number;
}

/** Team task tracking from team.task.* events */
export interface ActiveTeamTask {
  taskId: string;
  taskNumber: number;
  subject: string;
  status: string;
  ownerAgentKey?: string;
  ownerDisplayName?: string;
  progressPercent?: number;
  progressStep?: string;
  commentCount?: number;
  attachmentCount?: number;
}

/** Media item for gallery display */
export interface MediaItem {
  path: string;
  mimeType: string;
  fileName?: string;
  size?: number;
  kind: "image" | "video" | "audio" | "document" | "code";
  /** Prompt that generated this asset (create_image, native Codex image_generation_call). */
  prompt?: string;
}

/** Extended message with UI-specific fields */
export interface ChatMessage extends Message {
  timestamp?: number;
  isStreaming?: boolean;
  toolDetails?: ToolStreamEntry[];
  isBlockReply?: boolean;
  isNotification?: boolean;
  notificationType?: string;
  mediaItems?: MediaItem[];
}

/** Agent event payload from WS event "agent" */
export interface AgentEventPayload {
  type: string; // "run.started" | "run.completed" | "run.failed" | "run.cancelled" | "chunk" | "tool.call" | "tool.result" | "activity" | "block.reply" | "run.retrying"
  agentId: string;
  runId: string;
  runKind?: string; // "delegation" | "announce" — omitted for user-initiated runs
  channel?: string; // "ws", "telegram", "cron", etc.
  sessionKey?: string; // session this event belongs to
  payload?: {
    content?: string;
    name?: string;
    id?: string;
    is_error?: boolean;
    error?: string;
    arguments?: Record<string, unknown>;
    result?: string;
    // activity event fields
    phase?: string;
    tool?: string;
    tools?: string[];
    iteration?: number;
    // run.retrying event fields
    attempt?: number;
    maxAttempts?: number;
    // run.completed media files
    media?: { path: string; content_type?: string; size?: number }[];
  };
}

/** Tool call tracking during a chat run */
export interface ToolStreamEntry {
  toolCallId: string;
  runId: string;
  name: string;
  phase: "calling" | "completed" | "error";
  arguments?: Record<string, unknown>;
  result?: string;
  errorContent?: string;
  startedAt: number;
  updatedAt: number;
}

/** Chat send response from chat.send RPC */
export interface ChatSendResponse {
  runId: string;
  content: string;
  usage?: {
    promptTokens: number;
    completionTokens: number;
    totalTokens: number;
  };
}

/** Group of consecutive messages from the same role */
export interface MessageGroup {
  role: string;
  messages: ChatMessage[];
  timestamp: number;
  isStreaming: boolean;
}

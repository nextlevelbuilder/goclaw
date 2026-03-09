/** Chat-specific types for the chat page UI */

import type { Message } from "./session";

/** Extended message with UI-specific fields */
export interface ChatMessage extends Message {
  timestamp?: number;
  isStreaming?: boolean;
  toolDetails?: ToolStreamEntry[];
}

/** Agent event payload from WS event "agent" */
export interface AgentEventPayload {
  type: string; // "run.started" | "run.completed" | "run.failed" | "chunk" | "tool.call" | "tool.result"
  agentId: string;
  runId: string;
  runKind?: string; // "delegation" | "announce" — omitted for user-initiated runs
  payload?: {
    content?: string;
    name?: string;
    id?: string;
    is_error?: boolean;
    error?: string;
    arguments?: Record<string, unknown>;
    result?: string;
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

/** Session data types matching Go internal/store/session_store.go */

export interface SessionInfo {
  key: string;
  messageCount: number;
  created: string;
  updated: string;
  label?: string;
  model?: string;
  provider?: string;
  channel?: string;
  inputTokens?: number;
  outputTokens?: number;
  userID?: string;
  metadata?: Record<string, string>;
  agentName?: string;
  estimatedTokens?: number;
  contextWindow?: number;
  compactionCount?: number;
}

export interface SessionPreview {
  key: string;
  messages: Message[];
  summary?: string;
}

/** Message format from Go providers.Message */
export interface Message {
  role: "user" | "assistant" | "tool";
  content: string;
  thinking?: string;
  tool_calls?: ToolCall[];
  tool_call_id?: string;
  is_error?: boolean;
  media_refs?: { id: string; mime_type: string; kind: string; path?: string; prompt?: string }[];
  created_at?: string; // ISO 8601 timestamp from server; absent for older messages
}

export interface ToolCall {
  id: string;
  name: string;
  arguments: Record<string, unknown>;
}

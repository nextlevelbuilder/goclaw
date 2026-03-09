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
  tool_calls?: ToolCall[];
  tool_call_id?: string;
}

export interface ToolCall {
  id: string;
  name: string;
  arguments: Record<string, unknown>;
}

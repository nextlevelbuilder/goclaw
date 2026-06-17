export type RunTimelineItemType =
  | "activity"
  | "assistant.message"
  | "tool.call"
  | "tool.result"
  | "run.status";

export interface RunTimelineItem {
  id: string;
  tenant_id?: string;
  run_id: string;
  session_key: string;
  agent_id?: string;
  user_id?: string;
  channel?: string;
  chat_id?: string;
  seq: number;
  item_type: RunTimelineItemType;
  status?: "started" | "running" | "completed" | "failed" | "cancelled" | string;
  title?: string;
  preview?: string;
  tool_name?: string;
  tool_call_id?: string;
  trace_id?: string;
  span_id?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
}

export interface RunTimelineResponse {
  runId?: string;
  sessionKey?: string;
  items: RunTimelineItem[];
  limit: number;
  offset: number;
}

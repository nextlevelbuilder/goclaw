export interface TraceData {
  id: string;
  parent_trace_id?: string;
  agent_id?: string;
  user_id: string;
  session_key: string;
  run_id: string;
  start_time: string;
  end_time?: string;
  duration_ms: number;
  name: string;
  channel: string;
  input_preview: string;
  output_preview: string;
  total_input_tokens: number;
  total_output_tokens: number;
  total_cost: number;
  span_count: number;
  llm_call_count: number;
  tool_call_count: number;
  status: string;
  error?: string;
  tags?: string[];
  metadata?: { total_cache_read_tokens?: number; total_cache_creation_tokens?: number };
  created_at: string;
}

export interface SpanData {
  id: string;
  trace_id: string;
  parent_span_id?: string;
  agent_id?: string;
  span_type: string;
  name: string;
  start_time: string;
  end_time?: string;
  duration_ms: number;
  status: string;
  error?: string;
  model: string;
  provider: string;
  input_tokens: number;
  output_tokens: number;
  total_cost: number;
  finish_reason: string;
  tool_name: string;
  tool_call_id: string;
  input_preview: string;
  output_preview: string;
  metadata?: { cache_creation_tokens?: number; cache_read_tokens?: number; thinking_tokens?: number };
  created_at: string;
}

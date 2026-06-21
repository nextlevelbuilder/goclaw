export type TraceToolCallFilter = "true" | "false";

export interface TraceFilters {
  agentId?: string;
  userId?: string;
  status?: string;
  channel?: string;
  query?: string;
  from?: string;
  to?: string;
  minInputTokens?: string;
  maxInputTokens?: string;
  minOutputTokens?: string;
  maxOutputTokens?: string;
  minToolCalls?: string;
  maxToolCalls?: string;
  toolName?: string;
  hasToolCalls?: TraceToolCallFilter;
  limit?: number;
  offset?: number;
}

export interface TraceFilterChip {
  key: keyof TraceFilters;
  value: string;
}

const PARAMS: Array<[keyof TraceFilters, string]> = [
  ["query", "q"],
  ["agentId", "agent_id"],
  ["userId", "user_id"],
  ["status", "status"],
  ["channel", "channel"],
  ["from", "from"],
  ["to", "to"],
  ["minInputTokens", "min_input_tokens"],
  ["maxInputTokens", "max_input_tokens"],
  ["minOutputTokens", "min_output_tokens"],
  ["maxOutputTokens", "max_output_tokens"],
  ["minToolCalls", "min_tool_calls"],
  ["maxToolCalls", "max_tool_calls"],
  ["toolName", "tool_name"],
  ["hasToolCalls", "has_tool_calls"],
];

export function buildTraceRequestParams(filters: TraceFilters): Record<string, string> {
  const params: Record<string, string> = {};
  for (const [key, param] of PARAMS) {
    const value = filters[key];
    if (typeof value === "string" && value.trim() !== "") {
      params[param] = key === "from" || key === "to"
        ? toRFC3339(value.trim())
        : value.trim();
    }
  }
  if (filters.limit) params.limit = String(filters.limit);
  if (filters.offset !== undefined) params.offset = String(filters.offset);
  return params;
}

function toRFC3339(value: string): string {
  if (/[zZ]$|[+-]\d{2}:\d{2}$/.test(value)) {
    return value;
  }
  const parsed = new Date(value);
  return Number.isNaN(parsed.getTime()) ? value : parsed.toISOString();
}

export function getActiveTraceFilterChips(filters: TraceFilters): TraceFilterChip[] {
  return PARAMS
    .filter(([key]) => key !== "userId")
    .flatMap(([key]) => {
      const value = filters[key];
      return typeof value === "string" && value.trim() !== ""
        ? [{ key, value: value.trim() }]
        : [];
    });
}

export function clearTraceFilter(filters: TraceFilters, key: keyof TraceFilters): TraceFilters {
  const next = { ...filters };
  delete next[key];
  return next;
}

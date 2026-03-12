import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import type { UsageFilters } from "../context/usage-filter-context";

export interface SnapshotTimeSeries {
  bucket_time: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  thinking_tokens: number;
  request_count: number;
  llm_call_count: number;
  tool_call_count: number;
  error_count: number;
  avg_duration_ms: number;
  unique_users: number;
  memory_docs: number;
  memory_chunks: number;
  kg_entities: number;
  kg_relations: number;
  total_cost: number;
}

export interface SnapshotBreakdown {
  key: string;
  request_count: number;
  llm_call_count: number;
  input_tokens: number;
  output_tokens: number;
  error_count: number;
  avg_duration_ms: number;
  total_cost: number;
}

export interface SummaryData {
  requests: number;
  input_tokens: number;
  output_tokens: number;
  cost: number;
  errors: number;
  unique_users: number;
  llm_calls: number;
  tool_calls: number;
  avg_duration_ms: number;
}

interface SummaryResponse {
  current: SummaryData;
  previous: SummaryData;
}

function buildParams(filters: UsageFilters, extra?: Record<string, string>): Record<string, string> {
  const p: Record<string, string> = {
    from: filters.from,
    to: filters.to,
  };
  if (filters.agentId) p.agent_id = filters.agentId;
  if (filters.provider) p.provider = filters.provider;
  if (filters.model) p.model = filters.model;
  if (filters.channel) p.channel = filters.channel;
  return { ...p, ...extra };
}

// Stable query key: only values that affect the query, not the full filters object reference.
function filterKey(f: UsageFilters) {
  return [f.from, f.to, f.agentId, f.provider, f.model, f.channel] as const;
}

// Snapshots update hourly — no need to re-fetch on window focus or within 60s.
const STALE_TIME = 60_000;
const QUERY_OPTS = { staleTime: STALE_TIME, refetchOnWindowFocus: false } as const;

export function useUsageAnalytics(filters: UsageFilters) {
  const http = useHttp();
  const fk = filterKey(filters);

  const timeseriesQuery = useQuery({
    queryKey: ["usage", "timeseries", filters.granularity, ...fk],
    queryFn: () =>
      http.get<{ points: SnapshotTimeSeries[] }>("/v1/usage/timeseries", buildParams(filters, { group_by: filters.granularity })),
    placeholderData: (prev) => prev,
    ...QUERY_OPTS,
  });

  const providerQuery = useQuery({
    queryKey: ["usage", "breakdown", "provider", ...fk],
    queryFn: () =>
      http.get<{ rows: SnapshotBreakdown[] }>("/v1/usage/breakdown", buildParams(filters, { group_by: "provider" })),
    placeholderData: (prev) => prev,
    ...QUERY_OPTS,
  });

  const modelQuery = useQuery({
    queryKey: ["usage", "breakdown", "model", ...fk],
    queryFn: () =>
      http.get<{ rows: SnapshotBreakdown[] }>("/v1/usage/breakdown", buildParams(filters, { group_by: "model" })),
    placeholderData: (prev) => prev,
    ...QUERY_OPTS,
  });

  const channelQuery = useQuery({
    queryKey: ["usage", "breakdown", "channel", ...fk],
    queryFn: () =>
      http.get<{ rows: SnapshotBreakdown[] }>("/v1/usage/breakdown", buildParams(filters, { group_by: "channel" })),
    placeholderData: (prev) => prev,
    ...QUERY_OPTS,
  });

  const summaryQuery = useQuery({
    queryKey: ["usage", "summary", filters.period, ...fk],
    queryFn: () =>
      http.get<SummaryResponse>("/v1/usage/summary", buildParams(filters, { period: filters.period })),
    placeholderData: (prev) => prev,
    ...QUERY_OPTS,
  });

  // isLoading = first mount only (no cached data) → shows skeleton.
  // placeholderData keeps previous results visible during refetch → no flicker.
  const loading =
    timeseriesQuery.isLoading ||
    providerQuery.isLoading ||
    modelQuery.isLoading ||
    channelQuery.isLoading ||
    summaryQuery.isLoading;

  return {
    timeseries: timeseriesQuery.data?.points ?? [],
    providerBreakdown: providerQuery.data?.rows ?? [],
    modelBreakdown: modelQuery.data?.rows ?? [],
    channelBreakdown: channelQuery.data?.rows ?? [],
    summary: summaryQuery.data ?? null,
    loading,
    error: timeseriesQuery.error,
  };
}

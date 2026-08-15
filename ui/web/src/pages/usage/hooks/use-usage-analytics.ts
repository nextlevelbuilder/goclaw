import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { buildTimeRange, type UsageFilters } from "../context/usage-filter-context";

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
  const range = filters.period === "custom" ? filters : buildTimeRange(filters.period);
  const p: Record<string, string> = {
    from: range.from,
    to: range.to,
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

// Current-hour usage is filled from live traces, so keep the open dashboard fresh.
const REFRESH_INTERVAL = 30_000;
const QUERY_OPTS = {
  staleTime: REFRESH_INTERVAL,
  refetchInterval: REFRESH_INTERVAL,
  refetchIntervalInBackground: false,
  refetchOnWindowFocus: true,
} as const;

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

  const providerModelQuery = useQuery({
    queryKey: ["usage", "breakdown", "provider_model", ...fk],
    queryFn: () =>
      http.get<{ rows: SnapshotBreakdown[] }>("/v1/usage/breakdown", buildParams(filters, { group_by: "provider_model" })),
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

  const refreshAnalytics = useCallback(async () => {
    await Promise.all([
      timeseriesQuery.refetch(),
      providerQuery.refetch(),
      modelQuery.refetch(),
      providerModelQuery.refetch(),
      channelQuery.refetch(),
      summaryQuery.refetch(),
    ]);
  }, [
    timeseriesQuery.refetch,
    providerQuery.refetch,
    modelQuery.refetch,
    providerModelQuery.refetch,
    channelQuery.refetch,
    summaryQuery.refetch,
  ]);

  // isLoading = first mount only (no cached data) → shows skeleton.
  // placeholderData keeps previous results visible during refetch → no flicker.
  const loading =
    timeseriesQuery.isLoading ||
    providerQuery.isLoading ||
    modelQuery.isLoading ||
    providerModelQuery.isLoading ||
    channelQuery.isLoading ||
    summaryQuery.isLoading;
  const refreshing =
    timeseriesQuery.isFetching ||
    providerQuery.isFetching ||
    modelQuery.isFetching ||
    providerModelQuery.isFetching ||
    channelQuery.isFetching ||
    summaryQuery.isFetching;

  return {
    timeseries: timeseriesQuery.data?.points ?? [],
    providerBreakdown: providerQuery.data?.rows ?? [],
    modelBreakdown: modelQuery.data?.rows ?? [],
    providerModelBreakdown: providerModelQuery.data?.rows ?? [],
    channelBreakdown: channelQuery.data?.rows ?? [],
    summary: summaryQuery.data ?? null,
    loading,
    refreshing,
    refreshAnalytics,
    error: timeseriesQuery.error,
  };
}

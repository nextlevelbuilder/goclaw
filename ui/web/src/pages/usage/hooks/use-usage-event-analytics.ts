import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { buildTimeRange, type UsageFilters } from "../context/usage-filter-context";

export type UsageEventResourceType = "tool" | "skill" | "mcp_tool" | "runtime_tool";

export interface UsageEventSummary {
  calls: number;
  errors: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  cache_read_tokens: number;
  cache_create_tokens: number;
  thinking_tokens: number;
  cost_usd: number;
  avg_duration_ms: number;
}

export interface UsageEventBreakdown {
  key: string;
  event_type: string;
  resource_type: string;
  resource_name: string;
  source: string;
  calls: number;
  errors: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  cache_read_tokens: number;
  cache_create_tokens: number;
  thinking_tokens: number;
  cost_usd: number;
  avg_duration_ms: number;
}

export interface UsageEventTimeSeries {
  bucket_time: string;
  calls: number;
  errors: number;
  input_tokens: number;
  output_tokens: number;
  total_tokens: number;
  cache_read_tokens: number;
  cache_create_tokens: number;
  thinking_tokens: number;
  cost_usd: number;
  avg_duration_ms: number;
}

function buildParams(filters: UsageFilters, resourceType: UsageEventResourceType, extra?: Record<string, string>): Record<string, string> {
  const range = filters.period === "custom" ? filters : buildTimeRange(filters.period);
  const p: Record<string, string> = {
    from: range.from,
    to: range.to,
    resource_type: resourceType,
  };
  if (filters.agentId) p.agent_id = filters.agentId;
  if (filters.provider) p.provider = filters.provider;
  if (filters.model) p.model = filters.model;
  if (filters.channel) p.channel = filters.channel;
  return { ...p, ...extra };
}

function filterKey(f: UsageFilters, resourceType: UsageEventResourceType) {
  return [resourceType, f.from, f.to, f.agentId, f.provider, f.model, f.channel, f.granularity] as const;
}

const REFRESH_INTERVAL = 30_000;
const QUERY_OPTS = {
  staleTime: REFRESH_INTERVAL,
  refetchInterval: REFRESH_INTERVAL,
  refetchIntervalInBackground: false,
  refetchOnWindowFocus: true,
} as const;

export function useUsageEventAnalytics(filters: UsageFilters, resourceType: UsageEventResourceType) {
  const http = useHttp();
  const fk = filterKey(filters, resourceType);

  const summaryQuery = useQuery({
    queryKey: ["usage", "events", "summary", ...fk],
    queryFn: () =>
      http.get<{ summary: UsageEventSummary }>("/v1/usage/events/summary", buildParams(filters, resourceType)),
    placeholderData: (prev) => prev,
    ...QUERY_OPTS,
  });

  const breakdownQuery = useQuery({
    queryKey: ["usage", "events", "breakdown", "resource", ...fk],
    queryFn: () =>
      http.get<{ rows: UsageEventBreakdown[] }>(
        "/v1/usage/events/breakdown",
        buildParams(filters, resourceType, { group_by: "resource", limit: "25" }),
      ),
    placeholderData: (prev) => prev,
    ...QUERY_OPTS,
  });

  const sourceQuery = useQuery({
    queryKey: ["usage", "events", "breakdown", "source", ...fk],
    queryFn: () =>
      http.get<{ rows: UsageEventBreakdown[] }>(
        "/v1/usage/events/breakdown",
        buildParams(filters, resourceType, { group_by: "source", limit: "10" }),
      ),
    placeholderData: (prev) => prev,
    ...QUERY_OPTS,
  });

  const timeseriesQuery = useQuery({
    queryKey: ["usage", "events", "timeseries", ...fk],
    queryFn: () =>
      http.get<{ points: UsageEventTimeSeries[] }>(
        "/v1/usage/events/timeseries",
        buildParams(filters, resourceType, { group_by: filters.granularity }),
      ),
    placeholderData: (prev) => prev,
    ...QUERY_OPTS,
  });

  return {
    summary: summaryQuery.data?.summary ?? null,
    rows: breakdownQuery.data?.rows ?? [],
    sourceRows: sourceQuery.data?.rows ?? [],
    points: timeseriesQuery.data?.points ?? [],
    loading: summaryQuery.isLoading || breakdownQuery.isLoading || sourceQuery.isLoading || timeseriesQuery.isLoading,
    error: summaryQuery.error ?? breakdownQuery.error ?? sourceQuery.error ?? timeseriesQuery.error,
  };
}

import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import type { SnapshotTimeSeries, SummaryData } from "@/pages/usage/hooks/use-usage-analytics";

export const USAGE_RANGES = ["24h", "7d", "30d"] as const;
export type UsageRange = (typeof USAGE_RANGES)[number];
export const DEFAULT_USAGE_RANGE: UsageRange = "7d";

export function parseUsageRange(v: string | null | undefined): UsageRange {
  return (USAGE_RANGES as readonly string[]).includes(v ?? "") ? (v as UsageRange) : DEFAULT_USAGE_RANGE;
}

const RANGE_DAYS: Record<UsageRange, number> = { "24h": 1, "7d": 7, "30d": 30 };

export function rangeDays(range: UsageRange): number {
  return RANGE_DAYS[range];
}

/** RFC3339 window ending at `now` for the selected range. */
export function rangeWindow(range: UsageRange, now: number): { from: string; to: string } {
  return {
    from: new Date(now - RANGE_DAYS[range] * 86_400_000).toISOString(),
    to: new Date(now).toISOString(),
  };
}

// ---------------------------------------------------------------------------
// GET /v1/providers/{id}/usage-detail (see __fixtures__/provider-usage-detail.json)
// ---------------------------------------------------------------------------

export interface ProviderRecentError {
  at: string;
  error_class: string;
  count: number;
}

export interface ProviderUsageDetail {
  provider: string;
  calls: number;
  errors: number;
  latency_ms: { p50: number; p95: number };
  recent_errors: ProviderRecentError[];
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function asCount(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) && v > 0 ? v : 0;
}

/** Validates the usage-detail payload at the network boundary. */
export function parseProviderUsageDetail(raw: unknown): ProviderUsageDetail {
  if (!isRecord(raw)) throw new Error("invalid provider usage-detail response");
  const latency = isRecord(raw.latency_ms) ? raw.latency_ms : {};
  const recent: ProviderRecentError[] = [];
  if (Array.isArray(raw.recent_errors)) {
    for (const e of raw.recent_errors) {
      if (!isRecord(e) || typeof e.at !== "string" || Number.isNaN(Date.parse(e.at))) continue;
      recent.push({
        at: e.at,
        error_class: typeof e.error_class === "string" && e.error_class ? e.error_class : "unknown",
        count: asCount(e.count),
      });
    }
  }
  return {
    provider: typeof raw.provider === "string" ? raw.provider : "",
    calls: asCount(raw.calls),
    errors: asCount(raw.errors),
    latency_ms: { p50: asCount(latency.p50), p95: asCount(latency.p95) },
    recent_errors: recent,
  };
}

// ---------------------------------------------------------------------------
// Formatting helpers
// ---------------------------------------------------------------------------

/** Error fraction (0..1), or null when there were no calls. */
export function errorRate(errors: number, calls: number): number | null {
  if (!(calls > 0)) return null;
  return Math.min(1, Math.max(0, errors / calls));
}

export function formatErrorRate(rate: number | null): string {
  if (rate == null) return "—";
  const pct = rate * 100;
  if (pct > 0 && pct < 0.1) return "<0.1%";
  return `${pct.toFixed(1)}%`;
}

/** Latency in seconds with one decimal ("1.4 s"); sub-second values stay in ms. */
export function formatLatency(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms) || ms <= 0) return "—";
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(1)} s`;
}

export const KNOWN_ERROR_CLASSES = [
  "rate_limit",
  "overloaded",
  "timeout",
  "auth",
  "auth_permanent",
  "billing",
  "format",
  "model_not_found",
  "content_policy",
  "server_error",
  "context_overflow",
  "unknown",
] as const;
export type ErrorClass = (typeof KNOWN_ERROR_CLASSES)[number];

/** i18n key (providers namespace) for an error class; unknown classes fall back to `unknown`. */
export function errorClassKey(cls: string | null | undefined): `usage.errorClass.${ErrorClass}` {
  const known = (KNOWN_ERROR_CLASSES as readonly string[]).includes(cls ?? "") ? (cls as ErrorClass) : "unknown";
  return `usage.errorClass.${known}`;
}

// ---------------------------------------------------------------------------
// Query hook
// ---------------------------------------------------------------------------

interface SummaryResponse {
  current: SummaryData;
}

const USAGE_REFRESH_MS = 60_000;
const QUERY_OPTS = {
  staleTime: 30_000,
  gcTime: 10 * 60_000,
  refetchInterval: USAGE_REFRESH_MS,
  refetchIntervalInBackground: false,
} as const;

export const providerUsageKeys = {
  detail: (providerId: string, range: UsageRange) => ["providers", providerId, "usage-detail", range] as const,
  summary: (providerName: string, range: UsageRange) => ["providers", "usage-summary", providerName, range] as const,
  series: (providerName: string, range: UsageRange) => ["providers", "usage-series", providerName, range] as const,
};

/**
 * Loads the three data sets behind the provider Usage tab for one range.
 * Each range has its own cache entry, so switching back reuses cached data.
 * The time window is computed when the request runs, not in the query key.
 */
export function useProviderUsage(providerId: string, providerName: string, range: UsageRange, enabled = true) {
  const http = useHttp();
  const on = enabled && Boolean(providerId) && Boolean(providerName);

  const detailQuery = useQuery({
    queryKey: providerUsageKeys.detail(providerId, range),
    enabled: on,
    queryFn: async () => {
      const { from, to } = rangeWindow(range, Date.now());
      const raw = await http.get<unknown>(`/v1/providers/${providerId}/usage-detail`, { from, to, limit: "20" });
      return parseProviderUsageDetail(raw);
    },
    ...QUERY_OPTS,
  });

  const summaryQuery = useQuery({
    queryKey: providerUsageKeys.summary(providerName, range),
    enabled: on,
    queryFn: async () => {
      const res = await http.get<SummaryResponse>("/v1/usage/summary", { period: range, provider: providerName });
      return res?.current ?? null;
    },
    ...QUERY_OPTS,
  });

  const seriesQuery = useQuery({
    queryKey: providerUsageKeys.series(providerName, range),
    enabled: on,
    queryFn: async () => {
      const { from, to } = rangeWindow(range, Date.now());
      const res = await http.get<{ points: SnapshotTimeSeries[] | null }>("/v1/usage/timeseries", {
        from,
        to,
        group_by: "day",
        provider: providerName,
      });
      return res?.points ?? [];
    },
    ...QUERY_OPTS,
  });

  const queries = [detailQuery, summaryQuery, seriesQuery];
  const hasData = detailQuery.data !== undefined && summaryQuery.data !== undefined && seriesQuery.data !== undefined;
  const error = queries.find((q) => q.isError)?.error ?? null;
  const updatedAts = queries.map((q) => q.dataUpdatedAt).filter((t) => t > 0);

  return {
    detail: detailQuery.data,
    summary: summaryQuery.data ?? null,
    series: seriesQuery.data ?? [],
    hasData,
    isLoading: !hasData && queries.some((q) => q.isPending && q.fetchStatus === "fetching"),
    isFetching: queries.some((q) => q.isFetching),
    error,
    /** A refetch failed but cached data is still shown. */
    isStale: hasData && error != null,
    /** Oldest successful fetch time among the three data sets (ms epoch), 0 if none. */
    dataUpdatedAt: updatedAts.length ? Math.min(...updatedAts) : 0,
    refetch: () => Promise.all(queries.map((q) => q.refetch())),
  };
}

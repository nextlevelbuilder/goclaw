import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { TraceData, SpanData } from "@/types/trace";
import { buildTraceRequestParams, type TraceFilters } from "../trace-filter-params";

export type { TraceData, SpanData };
export type { TraceFilters };

export function useTraces(filters: TraceFilters = {}) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const queryKey = queryKeys.traces.list({ ...filters });

  const { data, isLoading: loading, isFetching } = useQuery({
    queryKey,
    queryFn: async () => {
      const params = buildTraceRequestParams(filters);
      const res = await http.get<{ traces: TraceData[]; total?: number }>("/v1/traces", params);
      return { traces: res.traces ?? [], total: res.total ?? 0 };
    },
    placeholderData: (prev) => prev,
    staleTime: 0,
  });

  const traces = data?.traces ?? [];
  const total = data?.total ?? 0;

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.traces.all }),
    [queryClient],
  );

  const getTrace = useCallback(
    async (traceId: string): Promise<{ trace: TraceData; spans: SpanData[] } | null> => {
      try {
        return await http.get<{ trace: TraceData; spans: SpanData[] }>(`/v1/traces/${traceId}`);
      } catch {
        return null;
      }
    },
    [http],
  );

  return { traces, total, loading, fetching: isFetching, refresh: invalidate, getTrace };
}

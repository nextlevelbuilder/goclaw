import { useState, useEffect, useRef } from "react";
import { useHttp } from "@/hooks/use-ws";

interface TimeSeriesPoint {
  bucket_time: string;
  input_tokens: number;
  output_tokens: number;
  total_cost: number;
  request_count: number;
}

interface SummaryData {
  requests: number;
  input_tokens: number;
  output_tokens: number;
  cost: number;
}

interface SummaryResponse {
  current: SummaryData;
  previous: SummaryData;
}

export interface OverviewSparklines {
  requestSparkline: number[];
  tokenSparkline: number[];
  costSparkline: number[];
  trends: {
    requests: number | null;
    tokens: number | null;
    cost: number | null;
  };
}

function computeTrend(current: number, previous: number): number | null {
  if (previous === 0) return current > 0 ? 100 : null;
  return Math.round(((current - previous) / previous) * 100);
}

export function useOverviewSparklines(): OverviewSparklines | null {
  const http = useHttp();
  const httpRef = useRef(http);
  httpRef.current = http;
  const [data, setData] = useState<OverviewSparklines | null>(null);
  const fetched = useRef(false);

  useEffect(() => {
    if (fetched.current) return;
    fetched.current = true;

    const load = async () => {
      try {
        const now = new Date();
        const from = new Date(now.getTime() - 24 * 60 * 60 * 1000);

        const [tsRes, sumRes] = await Promise.all([
          httpRef.current.get<{ points: TimeSeriesPoint[] }>("/v1/usage/timeseries", {
            from: from.toISOString(),
            to: now.toISOString(),
            group_by: "hour",
          }),
          httpRef.current.get<SummaryResponse>("/v1/usage/summary", { period: "today" }),
        ]);

        const points = tsRes.points ?? [];
        setData({
          requestSparkline: points.map((p) => p.request_count),
          tokenSparkline: points.map((p) => p.input_tokens + p.output_tokens),
          costSparkline: points.map((p) => p.total_cost),
          trends: {
            requests: computeTrend(sumRes.current.requests, sumRes.previous.requests),
            tokens: computeTrend(
              sumRes.current.input_tokens + sumRes.current.output_tokens,
              sumRes.previous.input_tokens + sumRes.previous.output_tokens,
            ),
            cost: computeTrend(sumRes.current.cost, sumRes.previous.cost),
          },
        });
      } catch {
        // graceful degradation — no sparklines shown
      }
    };

    load();
  }, []);

  return data;
}

import { useCallback, useState } from "react";
import { useWs } from "./use-ws";
import type { ApiError } from "@/api/errors";

interface WsCallResult<T> {
  data: T | null;
  loading: boolean;
  error: ApiError | null;
  call: (params?: Record<string, unknown>) => Promise<T>;
  reset: () => void;
}

export function useWsCall<T = unknown>(method: string): WsCallResult<T> {
  const ws = useWs();
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<ApiError | null>(null);

  const call = useCallback(
    async (params?: Record<string, unknown>): Promise<T> => {
      setLoading(true);
      setError(null);
      try {
        const result = await ws.call<T>(method, params);
        setData(result);
        return result;
      } catch (err) {
        setError(err as ApiError);
        throw err;
      } finally {
        setLoading(false);
      }
    },
    [ws, method],
  );

  const reset = useCallback(() => {
    setData(null);
    setError(null);
    setLoading(false);
  }, []);

  return { data, loading, error, call, reset };
}

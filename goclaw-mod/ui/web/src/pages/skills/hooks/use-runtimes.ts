import { useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { queryKeys } from "@/lib/query-keys";

export interface RuntimeInfo {
  name: string;
  available: boolean;
  version?: string;
}

export interface RuntimeStatus {
  runtimes: RuntimeInfo[];
  ready: boolean;
}

export function useRuntimes() {
  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);

  const { data, isPending: loading, refetch } = useQuery({
    queryKey: queryKeys.skills.runtimes,
    queryFn: () => http.get<RuntimeStatus>("/v1/skills/runtimes"),
    staleTime: 5 * 60_000,
    enabled: connected,
  });

  const refresh = useCallback(() => { refetch(); }, [refetch]);

  return { runtimes: data, loading, refresh };
}

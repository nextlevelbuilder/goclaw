import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { queryKeys } from "@/lib/query-keys";

export interface Proxy {
  id: string;
  name: string;
  url: string;
  username?: string;
  password?: string;
  geo?: string;
  isEnabled: boolean;
  isHealthy: boolean;
  failCount: number;
  lastHealthCheck?: string;
  createdAt: string;
}

interface AddProxyParams {
  name: string;
  url: string;
  username?: string;
  password?: string;
  geo?: string;
}

export function useProxyPool() {
  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);
  const queryClient = useQueryClient();

  const { data, isPending: loading, refetch } = useQuery({
    queryKey: queryKeys.proxyPool.all,
    queryFn: () => http.get<{ proxies: Proxy[] }>("/v1/browser/proxies"),
    enabled: connected,
  });

  const proxies = data?.proxies ?? [];

  const addMutation = useMutation({
    mutationFn: (params: AddProxyParams) =>
      http.post<{ ok: boolean; id: string }>("/v1/browser/proxies", params),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.proxyPool.all });
    },
  });

  const removeMutation = useMutation({
    mutationFn: (id: string) =>
      http.delete<{ ok: boolean }>(`/v1/browser/proxies/${id}`),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.proxyPool.all });
    },
  });

  const toggleMutation = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      http.patch<{ ok: boolean }>(`/v1/browser/proxies/${id}/toggle`, { enabled }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.proxyPool.all });
    },
  });

  const healthCheckMutation = useMutation({
    mutationFn: () =>
      http.post<{ ok: boolean }>("/v1/browser/proxies/health"),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.proxyPool.all });
    },
  });

  return {
    proxies,
    loading,
    addProxy: addMutation.mutateAsync,
    removeProxy: removeMutation.mutateAsync,
    toggleProxy: toggleMutation.mutateAsync,
    healthCheck: healthCheckMutation.mutateAsync,
    healthChecking: healthCheckMutation.isPending,
    refresh: refetch,
  };
}

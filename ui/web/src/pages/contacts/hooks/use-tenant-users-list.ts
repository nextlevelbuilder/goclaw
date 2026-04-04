import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { TenantUserData } from "@/types/tenant";

export function useTenantUsersList() {
  const http = useHttp();

  const { data, isLoading } = useQuery({
    queryKey: queryKeys.tenantUsers.all,
    queryFn: () => http.get<{ users: TenantUserData[] }>("/v1/tenant-users"),
    staleTime: 30_000,
  });

  return { users: data?.users ?? [], loading: isLoading };
}

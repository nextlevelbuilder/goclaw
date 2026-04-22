import { useCallback } from "react";
import { useHttp } from "@/hooks/use-ws";
import { useSseProgress, type UseSseProgressReturn } from "@/hooks/use-sse-progress";

type TenantRestoreExistingRequest = {
  mode: "upsert" | "replace";
  tenantId: string;
  dryRun?: boolean;
};

type TenantRestoreNewRequest = {
  mode: "new";
  newTenantSlug: string;
  dryRun?: boolean;
};

type TenantRestoreRequest = TenantRestoreExistingRequest | TenantRestoreNewRequest;

export interface TenantRestoreResult {
  tenant_id: string;
  tables_restored: Record<string, number>;
  files_extracted: number;
  warnings: string[];
  dry_run: boolean;
}

export interface UseTenantRestoreReturn extends UseSseProgressReturn {
  startRestore: (file: File, request: TenantRestoreRequest) => void;
}

export function useTenantRestore(): UseTenantRestoreReturn {
  const http = useHttp();
  const authHeaders = useCallback(() => http.getAuthHeaders(), [http]);
  const sse = useSseProgress(authHeaders);

  const startRestore = useCallback(
    (file: File, request: TenantRestoreRequest) => {
      const params = new URLSearchParams({ mode: request.mode, stream: "true" });

      if (request.mode === "new") {
        params.set("tenant_slug", request.newTenantSlug);
      } else {
        params.set("tenant_id", request.tenantId);
      }

      if (request.dryRun) params.set("dry_run", "true");

      const url = `${window.location.origin}/v1/tenant/restore?${params}`;
      const formData = new FormData();
      formData.append("archive", file);

      sse.startPost(url, formData);
    },
    [sse],
  );

  return { ...sse, startRestore };
}

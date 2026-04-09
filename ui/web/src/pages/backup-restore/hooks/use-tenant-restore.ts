import { useCallback } from "react";
import { useHttp } from "@/hooks/use-ws";
import { useSseProgress, type UseSseProgressReturn } from "@/hooks/use-sse-progress";

export interface TenantRestoreResult {
  tenant_id: string;
  tables_restored: Record<string, number>;
  files_extracted: number;
  warnings: string[];
  dry_run: boolean;
}

export interface UseTenantRestoreReturn extends UseSseProgressReturn {
  startRestore: (file: File, tenantId: string, opts: { mode: string; dryRun?: boolean }) => void;
}

export function useTenantRestore(): UseTenantRestoreReturn {
  const http = useHttp();
  const authHeaders = useCallback(() => http.getAuthHeaders(), [http]);
  const sse = useSseProgress(authHeaders);

  const startRestore = useCallback(
    (file: File, tenantId: string, opts: { mode: string; dryRun?: boolean }) => {
      const params = new URLSearchParams({
        tenant_id: tenantId,
        mode: opts.mode,
        stream: "true",
      });
      if (opts.dryRun) params.set("dry_run", "true");

      const url = `${window.location.origin}/v1/tenant/restore?${params}`;
      const formData = new FormData();
      formData.append("archive", file);

      sse.startPost(url, formData);
    },
    [sse],
  );

  return { ...sse, startRestore };
}

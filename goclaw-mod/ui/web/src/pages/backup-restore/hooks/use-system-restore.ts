import { useCallback } from "react";
import { useHttp } from "@/hooks/use-ws";
import { useSseProgress, type UseSseProgressReturn } from "@/hooks/use-sse-progress";

export interface RestoreResult {
  manifest_version: number;
  schema_version: number;
  database_restored: boolean;
  files_extracted: number;
  bytes_extracted: number;
  warnings: string[];
  dry_run: boolean;
}

export interface UseSystemRestoreReturn extends UseSseProgressReturn {
  startRestore: (file: File, opts: { skipDb?: boolean; skipFiles?: boolean; dryRun?: boolean }) => void;
}

export function useSystemRestore(): UseSystemRestoreReturn {
  const http = useHttp();
  const authHeaders = useCallback(() => http.getAuthHeaders(), [http]);
  const sse = useSseProgress(authHeaders);

  const startRestore = useCallback(
    (file: File, opts: { skipDb?: boolean; skipFiles?: boolean; dryRun?: boolean }) => {
      const params = new URLSearchParams({ stream: "true" });
      if (opts.skipDb) params.set("skip_db", "true");
      if (opts.skipFiles) params.set("skip_files", "true");
      if (opts.dryRun) params.set("dry_run", "true");

      const url = `${window.location.origin}/v1/system/restore?${params}`;
      const formData = new FormData();
      formData.append("archive", file);

      sse.startPost(url, formData);
    },
    [sse],
  );

  return { ...sse, startRestore };
}

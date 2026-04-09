import { useState, useCallback, useEffect } from "react";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import { useSseProgress, type UseSseProgressReturn } from "@/hooks/use-sse-progress";

export interface UseTenantBackupReturn extends UseSseProgressReturn {
  startBackup: (tenantId: string) => void;
  downloadReady: boolean;
  download: () => void;
}

export function useTenantBackup(): UseTenantBackupReturn {
  const http = useHttp();
  const [downloadUrl, setDownloadUrl] = useState<string | null>(null);
  const [downloadName, setDownloadName] = useState("");

  const authHeaders = useCallback(() => http.getAuthHeaders(), [http]);
  const sse = useSseProgress(authHeaders);

  useEffect(() => {
    if (sse.result?.download_url && !downloadUrl) {
      setDownloadUrl(sse.result.download_url as string);
      setDownloadName((sse.result.file_name as string) ?? "tenant-backup.tar.gz");
    }
  }, [sse.result, downloadUrl]);

  const startBackup = useCallback(
    (tenantId: string) => {
      setDownloadUrl(null);
      setDownloadName("");
      const params = new URLSearchParams({ tenant_id: tenantId, stream: "true" });
      const url = `${window.location.origin}/v1/tenant/backup?${params}`;
      sse.startPost(url, new FormData());
    },
    [sse],
  );

  const download = useCallback(async () => {
    if (!downloadUrl) return;
    try {
      const blob = await http.downloadBlob(downloadUrl);
      const a = document.createElement("a");
      a.href = URL.createObjectURL(blob);
      a.download = downloadName;
      a.click();
      URL.revokeObjectURL(a.href);
    } catch {
      toast.error("Download failed");
    }
  }, [downloadUrl, downloadName, http]);

  return { ...sse, startBackup, downloadReady: !!downloadUrl, download };
}

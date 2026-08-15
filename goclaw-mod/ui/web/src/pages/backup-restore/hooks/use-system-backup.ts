import { useState, useCallback, useEffect } from "react";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import { useSseProgress, type UseSseProgressReturn } from "@/hooks/use-sse-progress";

export interface UseSystemBackupReturn extends UseSseProgressReturn {
  startBackup: (opts?: { toS3?: boolean }) => void;
  downloadReady: boolean;
  download: () => void;
  backupToken: string | null;
  uploadToS3: () => void;
}

/**
 * System backup hook. Uses SSE for progress tracking.
 * Backend POST /v1/system/backup uses decodeJSONOptional which silently
 * defaults to include everything (exclude_db=false, exclude_files=false)
 * when body is not JSON. We send empty FormData which triggers these defaults.
 */
export function useSystemBackup(): UseSystemBackupReturn {
  const http = useHttp();
  const [downloadUrl, setDownloadUrl] = useState<string | null>(null);
  const [downloadName, setDownloadName] = useState("");
  const [backupToken, setBackupToken] = useState<string | null>(null);

  const authHeaders = useCallback(() => http.getAuthHeaders(), [http]);
  const sse = useSseProgress(authHeaders);

  useEffect(() => {
    if (sse.result?.download_url && !downloadUrl) {
      setDownloadUrl(sse.result.download_url as string);
      setDownloadName((sse.result.file_name as string) ?? "backup.tar.gz");
      const parts = (sse.result.download_url as string).split("/");
      setBackupToken(parts[parts.length - 1] ?? null);
    }
  }, [sse.result, downloadUrl]);

  const startBackup = useCallback(
    (opts?: { toS3?: boolean }) => {
      setDownloadUrl(null);
      setDownloadName("");
      setBackupToken(null);

      const endpoint = opts?.toS3
        ? "/v1/system/backup/s3/backup"
        : "/v1/system/backup";

      const url = `${window.location.origin}${endpoint}?stream=true`;
      // Backend decodeJSONOptional defaults to include everything with empty FormData
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

  const uploadToS3 = useCallback(() => {
    if (!backupToken) return;
    const url = `${window.location.origin}/v1/system/backup/s3/upload?stream=true&backup_token=${backupToken}`;
    sse.startPost(url, new FormData());
  }, [backupToken, sse]);

  return { ...sse, startBackup, downloadReady: !!downloadUrl, download, backupToken, uploadToS3 };
}

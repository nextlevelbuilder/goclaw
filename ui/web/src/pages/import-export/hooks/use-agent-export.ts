import { useState, useCallback, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import { useSseProgress, type UseSseProgressReturn } from "@/hooks/use-sse-progress";

export interface ExportPreview {
  context_files: number;
  user_context_files_users: number;
  memory_global: number;
  memory_per_user: number;
  kg_entities: number;
  kg_relations: number;
  user_profiles: number;
  user_overrides: number;
  cron_jobs: number;
  workspace_files: number;
}

export function useExportPreview(agentId: string | null) {
  const http = useHttp();
  return useQuery({
    queryKey: ["export-preview", agentId],
    enabled: !!agentId,
    queryFn: () => http.get<ExportPreview>(`/v1/agents/${agentId}/export/preview`),
    staleTime: 30_000,
  });
}

export function useExport(): UseSseProgressReturn & {
  startExport: (agentId: string, sections: string[]) => void;
  downloadReady: boolean;
  download: () => void;
} {
  const http = useHttp();
  const [downloadUrl, setDownloadUrl] = useState<string | null>(null);
  const [downloadName, setDownloadName] = useState("");

  const authHeaders = useCallback(() => http.getAuthHeaders(), [http]);
  const sse = useSseProgress(authHeaders);

  useEffect(() => {
    if (sse.result?.download_url && !downloadUrl) {
      setDownloadUrl(sse.result.download_url);
      setDownloadName(sse.result.file_name ?? "export.tar.gz");
    }
  }, [sse.result, downloadUrl]);

  const startExport = useCallback(
    (agentId: string, sections: string[]) => {
      setDownloadUrl(null);
      setDownloadName("");
      const params = new URLSearchParams({ sections: sections.join(","), stream: "true" });
      const url = `${window.location.origin}/v1/agents/${agentId}/export?${params}`;
      sse.startGet(url);
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

  return { ...sse, startExport, downloadReady: !!downloadUrl, download };
}

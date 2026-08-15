import { useState, useCallback, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import { useSseProgress, type UseSseProgressReturn } from "@/hooks/use-sse-progress";

export interface TeamExportPreview {
  team_name: string;
  team_id: string;
  tasks: number;
  members: number;
  agent_links: number;
  agent_count: number;
}

export function useTeamExportPreview(teamId: string | null) {
  const http = useHttp();
  return useQuery({
    queryKey: ["team-export-preview", teamId],
    enabled: !!teamId,
    queryFn: () => http.get<TeamExportPreview>(`/v1/teams/${teamId}/export/preview`),
    staleTime: 60_000,
  });
}

export function useTeamExport(): UseSseProgressReturn & {
  startExport: (teamId: string) => void;
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
      setDownloadName(sse.result.file_name ?? "team-export.tar.gz");
    }
  }, [sse.result, downloadUrl]);

  const startExport = useCallback(
    (teamId: string) => {
      setDownloadUrl(null);
      setDownloadName("");
      const url = `${window.location.origin}/v1/teams/${teamId}/export?stream=true`;
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

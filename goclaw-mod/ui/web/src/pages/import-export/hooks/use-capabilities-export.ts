import { useState, useCallback, useEffect } from "react";
import { useQuery } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import { useSseProgress, type UseSseProgressReturn } from "@/hooks/use-sse-progress";

export interface SkillsExportPreview {
  custom_skills: number;
  total_grants: number;
}

export interface McpExportPreview {
  servers: number;
  agent_grants: number;
}

export function useSkillsExportPreview() {
  const http = useHttp();
  return useQuery({
    queryKey: ["skills-export-preview"],
    queryFn: () => http.get<SkillsExportPreview>("/v1/skills/export/preview"),
    staleTime: 60_000,
  });
}

export function useMcpExportPreview() {
  const http = useHttp();
  return useQuery({
    queryKey: ["mcp-export-preview"],
    queryFn: () => http.get<McpExportPreview>("/v1/mcp/export/preview"),
    staleTime: 60_000,
  });
}

function useCapabilityExport(defaultFileName: string): UseSseProgressReturn & {
  startExport: (url: string) => void;
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
      setDownloadName(sse.result.file_name ?? defaultFileName);
    }
  }, [sse.result, downloadUrl, defaultFileName]);

  const startExport = useCallback(
    (exportUrl: string) => {
      setDownloadUrl(null);
      setDownloadName("");
      sse.startGet(exportUrl);
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

export function useSkillsExport() {
  const exp = useCapabilityExport("skills-export.tar.gz");
  return {
    ...exp,
    startSkillsExport: () => {
      exp.startExport(`${window.location.origin}/v1/skills/export?stream=true`);
    },
  };
}

export function useMcpExport() {
  const exp = useCapabilityExport("mcp-export.tar.gz");
  return {
    ...exp,
    startMcpExport: () => {
      exp.startExport(`${window.location.origin}/v1/mcp/export?stream=true`);
    },
  };
}

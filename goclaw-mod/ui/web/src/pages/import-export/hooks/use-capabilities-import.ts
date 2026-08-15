import { useCallback } from "react";
import { useHttp } from "@/hooks/use-ws";
import { useSseProgress, type UseSseProgressReturn } from "@/hooks/use-sse-progress";

export function useSkillsImport(): UseSseProgressReturn & {
  startImport: (file: File) => void;
} {
  const http = useHttp();
  const authHeaders = useCallback(() => http.getAuthHeaders(), [http]);
  const sse = useSseProgress(authHeaders);

  const startImport = useCallback(
    (file: File) => {
      const form = new FormData();
      form.append("file", file);
      const url = `${window.location.origin}/v1/skills/import?stream=true`;
      sse.startPost(url, form);
    },
    [sse],
  );

  return { ...sse, startImport };
}

export function useMcpImport(): UseSseProgressReturn & {
  startImport: (file: File) => void;
} {
  const http = useHttp();
  const authHeaders = useCallback(() => http.getAuthHeaders(), [http]);
  const sse = useSseProgress(authHeaders);

  const startImport = useCallback(
    (file: File) => {
      const form = new FormData();
      form.append("file", file);
      const url = `${window.location.origin}/v1/mcp/import?stream=true`;
      sse.startPost(url, form);
    },
    [sse],
  );

  return { ...sse, startImport };
}

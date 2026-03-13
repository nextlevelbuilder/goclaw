import { useCallback, useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWs, useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";
import { ApiError } from "@/api/errors";
import { queryKeys } from "@/lib/query-keys";
import type { ManagedToolInfo, ManagedToolFile, ManagedToolVersions } from "@/types/tool";

export type { ManagedToolInfo, ManagedToolFile, ManagedToolVersions };

export function useManagedTools() {
  const ws = useWs();
  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);
  const token = useAuthStore((s) => s.token);
  const userId = useAuthStore((s) => s.userId);
  const queryClient = useQueryClient();

  const { data: managedTools = [], isFetching: loading } = useQuery({
    queryKey: queryKeys.managedTools.all,
    queryFn: async () => {
      const res = await ws.call<{ managed_tools: ManagedToolInfo[] }>(Methods.MANAGED_TOOLS_LIST);
      return res.managed_tools ?? [];
    },
    staleTime: 60_000,
    enabled: connected,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.managedTools.all }),
    [queryClient],
  );

  useEffect(() => {
    if (connected) invalidate();
  }, [connected]); // eslint-disable-line react-hooks/exhaustive-deps

  const uploadTool = useCallback(
    async (file: File) => {
      const formData = new FormData();
      formData.append("file", file);
      const res = await http.upload<{ id: string; slug: string; version: number; name: string }>(
        "/v1/managed-tools/upload",
        formData,
      );
      await invalidate();
      return res;
    },
    [http, invalidate],
  );

  const updateTool = useCallback(
    async (id: string, updates: Record<string, unknown>) => {
      const res = await http.put<{ ok: string }>(`/v1/managed-tools/${id}`, updates);
      await invalidate();
      return res;
    },
    [http, invalidate],
  );

  const deleteTool = useCallback(
    async (id: string) => {
      const res = await http.delete<{ ok: string }>(`/v1/managed-tools/${id}`);
      await invalidate();
      return res;
    },
    [http, invalidate],
  );

  const toggleTool = useCallback(
    async (id: string, enabled: boolean) => {
      const res = await http.post<{ ok: boolean; enabled: boolean; status: string }>(
        `/v1/managed-tools/${id}/toggle`,
        { enabled },
      );
      await invalidate();
      return res;
    },
    [http, invalidate],
  );

  const getManagedTool = useCallback(
    (id: string) => http.get<ManagedToolInfo>(`/v1/managed-tools/${id}`),
    [http],
  );

  const getFiles = useCallback(
    async (id: string, version?: number) => {
      const q = version != null ? `?version=${version}` : "";
      const res = await http.get<{ files: ManagedToolFile[] }>(`/v1/managed-tools/${id}/files${q}`);
      return res.files ?? [];
    },
    [http],
  );

  const getFileContent = useCallback(
    async (id: string, path: string, version?: number) => {
      const q = version != null ? `?version=${version}` : "";
      return http.get<{ content: string; path: string; size: number }>(
        `/v1/managed-tools/${id}/files/${encodeURIComponent(path)}${q}`,
      );
    },
    [http],
  );

  const getVersions = useCallback(
    async (id: string) => {
      return http.get<ManagedToolVersions>(`/v1/managed-tools/${id}/versions`);
    },
    [http],
  );

  const writeFile = useCallback(
    async (id: string, filePath: string, content: string): Promise<void> => {
      const encoded = encodeURIComponent(filePath);
      const url = new URL(`/v1/managed-tools/${id}/files/${encoded}`, window.location.origin);

      const headers: Record<string, string> = {
        "Content-Type": "text/plain; charset=utf-8",
      };
      if (token) headers["Authorization"] = `Bearer ${token}`;
      if (userId) headers["X-GoClaw-User-Id"] = userId;

      const res = await fetch(url.toString(), {
        method: "PUT",
        headers,
        body: content,
      });

      if (!res.ok) {
        const err = await res.json().catch(() => ({ error: res.statusText }));
        throw new ApiError(
          err.code ?? "HTTP_ERROR",
          err.error ?? err.message ?? res.statusText,
        );
      }
    },
    [token, userId],
  );

  return {
    managedTools,
    loading,
    refresh: invalidate,
    uploadTool,
    updateTool,
    deleteTool,
    toggleTool,
    getManagedTool,
    getFiles,
    getManagedToolFiles: getFiles,
    getFileContent,
    getVersions,
    writeFile,
  };
}

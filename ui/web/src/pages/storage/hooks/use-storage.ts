import { useState, useCallback, useEffect, useRef } from "react";
import { useHttp } from "@/hooks/use-ws";
import { toast } from "@/stores/use-toast-store";
import i18next from "i18next";
import { userFriendlyError } from "@/lib/error-utils";

export interface StorageFile {
  path: string;
  name: string;
  isDir: boolean;
  size: number;
  hasChildren?: boolean;
  protected: boolean;
}

interface StorageListResponse {
  files: StorageFile[];
  baseDir: string;
}

interface StorageFileContent {
  content: string;
  path: string;
  size: number;
}

export function useStorage() {
  const http = useHttp();
  const [files, setFiles] = useState<StorageFile[]>([]);
  const [baseDir, setBaseDir] = useState("");
  const [loading, setLoading] = useState(false);

  const listFiles = useCallback(async () => {
    setLoading(true);
    try {
      const res = await http.get<StorageListResponse>("/v1/storage/files");
      setFiles(res.files ?? []);
      setBaseDir(res.baseDir ?? "");
    } finally {
      setLoading(false);
    }
  }, [http]);

  const loadSubtree = useCallback(async (path: string): Promise<StorageFile[]> => {
    const res = await http.get<StorageListResponse>("/v1/storage/files", { path });
    return res.files ?? [];
  }, [http]);

  const readFile = useCallback(
    async (path: string) => {
      return http.get<StorageFileContent>(
        `/v1/storage/files/${encodeURIComponent(path)}`,
      );
    },
    [http],
  );

  const deleteFile = useCallback(
    async (path: string) => {
      try {
        await http.delete<{ status: string }>(
          `/v1/storage/files/${encodeURIComponent(path)}`,
        );
        toast.success(i18next.t("storage:toast.deleted"));
      } catch (err) {
        toast.error(i18next.t("storage:toast.deleteFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [http],
  );

  /** Fetch raw file as blob (for images, downloads). */
  const fetchRawBlob = useCallback(
    (path: string, download?: boolean) => {
      const params: Record<string, string> = { raw: "true" };
      if (download) params.download = "true";
      return http.fetchBlob(`/v1/storage/files/${encodeURIComponent(path)}`, params);
    },
    [http],
  );

  return { files, baseDir, loading, listFiles, loadSubtree, readFile, deleteFile, fetchRawBlob };
}

interface SizeState {
  totalSize: number;
  fileCount: number;
  loading: boolean;
  cached: boolean;
}

export function useStorageSize() {
  const http = useHttp();
  const [state, setState] = useState<SizeState>({ totalSize: 0, fileCount: 0, loading: false, cached: false });
  const abortRef = useRef<AbortController | null>(null);

  const fetchSize = useCallback(async () => {
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;

    setState((s) => ({ ...s, loading: true, totalSize: 0, fileCount: 0 }));

    try {
      const res = await http.streamFetch("/v1/storage/size", controller.signal);
      if (!res.body) {
        setState((s) => ({ ...s, loading: false }));
        return;
      }

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buffer = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split("\n");
        buffer = lines.pop() ?? "";

        for (const line of lines) {
          if (!line.startsWith("data: ")) continue;
          try {
            const data = JSON.parse(line.slice(6));
            if (data.done) {
              setState({ totalSize: data.total, fileCount: data.files, loading: false, cached: !!data.cached });
            } else {
              setState((s) => ({ ...s, totalSize: data.current, fileCount: data.files }));
            }
          } catch { /* skip malformed */ }
        }
      }
    } catch (err) {
      if (err instanceof DOMException && err.name === "AbortError") return;
      setState((s) => ({ ...s, loading: false }));
    }
  }, [http]);

  useEffect(() => {
    return () => abortRef.current?.abort();
  }, []);

  return { ...state, refreshSize: fetchSize };
}

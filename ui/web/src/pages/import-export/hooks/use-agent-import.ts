import { useState, useCallback } from "react";
import { useHttp } from "@/hooks/use-ws";
import { useSseProgress, type UseSseProgressReturn } from "@/hooks/use-sse-progress";

export type ImportMode = "new" | "merge";

export interface ImportManifest {
  version: number;
  format: string;
  exported_at: string;
  agent_key: string;
  sections: Record<string, unknown>;
}

export function useImport(): UseSseProgressReturn & {
  manifest: ImportManifest | null;
  parseFile: (file: File) => Promise<ImportManifest | null>;
  startImport: (file: File, overrides?: { agent_key?: string; display_name?: string }) => void;
  startMerge: (file: File, agentId: string, sections: string[]) => void;
  clearFile: () => void;
} {
  const http = useHttp();
  const [manifest, setManifest] = useState<ImportManifest | null>(null);

  const authHeaders = useCallback(() => http.getAuthHeaders(), [http]);
  const sse = useSseProgress(authHeaders);

  const parseFile = useCallback(async (file: File): Promise<ImportManifest | null> => {
    if (file.name.endsWith(".agent.json") || file.name.endsWith(".json")) {
      try {
        const text = await file.text();
        const json = JSON.parse(text);
        const m: ImportManifest = {
          version: json.version ?? 1,
          format: json.format ?? "legacy-json",
          exported_at: json.exported_at ?? "",
          agent_key: json.agent?.agent_key ?? "",
          sections: {
            agent_config: !!json.agent,
            context_files: (json.context_files?.length ?? 0) > 0,
            memory: (json.memories?.length ?? 0) > 0,
            knowledge_graph: !!(json.knowledge_graph?.entities?.length),
          },
        };
        setManifest(m);
        return m;
      } catch {
        return null;
      }
    }

    try {
      const form = new FormData();
      form.append("file", file);
      const res = await fetch(
        `${window.location.origin}/v1/agents/import/preview`,
        { method: "POST", headers: authHeaders(), body: form },
      );
      if (!res.ok) return null;
      const m = (await res.json()) as ImportManifest;
      setManifest(m);
      return m;
    } catch {
      return null;
    }
  }, [authHeaders]);

  const startImport = useCallback(
    (file: File, overrides?: { agent_key?: string; display_name?: string }) => {
      const form = new FormData();
      form.append("file", file);
      if (overrides?.agent_key) form.append("agent_key", overrides.agent_key);
      if (overrides?.display_name) form.append("display_name", overrides.display_name);

      const url = `${window.location.origin}/v1/agents/import?stream=true`;
      sse.startPost(url, form);
    },
    [sse],
  );

  const startMerge = useCallback(
    (file: File, agentId: string, sections: string[]) => {
      const form = new FormData();
      form.append("file", file);

      const params = new URLSearchParams({
        include: sections.join(","),
        stream: "true",
      });
      const url = `${window.location.origin}/v1/agents/${agentId}/import?${params}`;
      sse.startPost(url, form);
    },
    [sse],
  );

  const clearFile = useCallback(() => {
    setManifest(null);
    sse.reset();
  }, [sse]);

  return { ...sse, manifest, parseFile, startImport, startMerge, clearFile };
}

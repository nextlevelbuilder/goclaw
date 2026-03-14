import { useState, useCallback } from "react";
import { useHttp } from "@/hooks/use-ws";
import type { TreeNode } from "@/lib/file-helpers";

export interface MCPBuilderProject {
  id: string;
  name: string;
  created_at: string;
}

export function useMCPBuilder() {
  const http = useHttp();

  const [projectId, setProjectId] = useState<string | null>(null);
  const [tree, setTree] = useState<TreeNode[]>([]);
  const [filesLoading, setFilesLoading] = useState(false);
  const [fileContent, setFileContent] = useState<string | null>(null);
  const [fileContentLoading, setFileContentLoading] = useState(false);

  const createProject = useCallback(
    async (name: string): Promise<string | null> => {
      try {
        const res = await http.post<{ id: string; name: string }>(
          "/v1/mcp/builder/projects",
          { name },
        );
        setProjectId(res.id);
        return res.id;
      } catch (err: unknown) {
        // 409 = project already exists, treat as success
        if (err && typeof err === "object" && "status" in err && (err as { status: number }).status === 409) {
          setProjectId(name);
          return name;
        }
        throw err;
      }
    },
    [http],
  );

  const fetchFileTree = useCallback(
    async (id: string) => {
      setFilesLoading(true);
      try {
        const res = await http.get<{ tree: TreeNode[] }>(
          `/v1/mcp/builder/projects/${id}/files`,
        );
        setTree(res.tree ?? []);
      } catch {
        setTree([]);
      } finally {
        setFilesLoading(false);
      }
    },
    [http],
  );

  const fetchFileContent = useCallback(
    async (id: string, path: string) => {
      setFileContentLoading(true);
      try {
        const res = await http.get<{ content: string }>(
          `/v1/mcp/builder/projects/${id}/file?path=${encodeURIComponent(path)}`,
        );
        setFileContent(res.content ?? "");
      } catch {
        setFileContent(null);
      } finally {
        setFileContentLoading(false);
      }
    },
    [http],
  );

  const registerProject = useCallback(
    async (id: string) => {
      await http.post(`/v1/mcp/builder/projects/${id}/register`, {});
    },
    [http],
  );

  return {
    projectId,
    setProjectId,
    tree,
    filesLoading,
    fileContent,
    fileContentLoading,
    createProject,
    fetchFileTree,
    fetchFileContent,
    registerProject,
  };
}

import { useState, useCallback } from "react";
import { useHttp } from "@/hooks/use-ws";
import { buildTree, mergeSubtree, setNodeLoading, type TreeNode } from "@/lib/file-helpers";

export interface VaultTreeEntry {
  name: string;
  path: string;
  isDir: boolean;
  hasChildren?: boolean;
  docId?: string;
  docType?: string;
  scope?: string;
  title?: string;
  updatedAt?: string;
}

interface VaultTreeResponse { entries: VaultTreeEntry[] }

interface VaultTreeFilter {
  agent_id?: string;
  scope?: string;
  doc_type?: string;
  team_id?: string;
}

function toTreeInputs(entries: VaultTreeEntry[]) {
  return entries.map((e) => ({
    path: e.path, name: e.name, isDir: e.isDir, size: 0, hasChildren: e.hasChildren,
  }));
}

export function useVaultTree(filter: VaultTreeFilter = {}) {
  const http = useHttp();
  const [tree, setTree] = useState<TreeNode[]>([]);
  const [meta, setMeta] = useState<Map<string, VaultTreeEntry>>(new Map());
  const [loading, setLoading] = useState(false);

  const buildParams = useCallback(
    (extraPath?: string): Record<string, string> => {
      const p: Record<string, string> = {};
      if (extraPath) p.path = extraPath;
      if (filter.agent_id) p.agent_id = filter.agent_id;
      if (filter.scope) p.scope = filter.scope;
      if (filter.doc_type) p.doc_type = filter.doc_type;
      if (filter.team_id) p.team_id = filter.team_id;
      return p;
    },
    [filter.agent_id, filter.scope, filter.doc_type, filter.team_id],
  );

  const loadRoot = useCallback(async () => {
    setLoading(true);
    try {
      const res = await http.get<VaultTreeResponse>("/v1/vault/tree", buildParams());
      const entries = res.entries ?? [];
      const m = new Map<string, VaultTreeEntry>();
      for (const e of entries) m.set(e.path, e);
      setMeta(m);
      setTree(buildTree(toTreeInputs(entries)));
    } finally {
      setLoading(false);
    }
  }, [http, buildParams]);

  const loadSubtree = useCallback(async (path: string) => {
    setTree((prev) => setNodeLoading(prev, path, true));
    try {
      const res = await http.get<VaultTreeResponse>("/v1/vault/tree", buildParams(path));
      const entries = res.entries ?? [];
      setMeta((prev) => {
        const next = new Map(prev);
        for (const e of entries) next.set(e.path, e);
        return next;
      });
      setTree((prev) => mergeSubtree(prev, path, toTreeInputs(entries)));
    } catch {
      setTree((prev) => setNodeLoading(prev, path, false));
    }
  }, [http, buildParams]);

  return { tree, meta, loading, loadRoot, loadSubtree };
}

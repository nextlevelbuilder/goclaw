import { useCallback, useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWs, useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";
import { queryKeys } from "@/lib/query-keys";
import type { SkillInfo, SkillFile, SkillVersions } from "@/types/skill";

export type { SkillInfo, SkillFile, SkillVersions };

export function useSkills() {
  const ws = useWs();
  const http = useHttp();
  const connected = useAuthStore((s) => s.connected);
  const queryClient = useQueryClient();

  const { data: skills = [], isFetching: loading } = useQuery({
    queryKey: queryKeys.skills.all,
    queryFn: async () => {
      const res = await ws.call<{ skills: SkillInfo[] }>(Methods.SKILLS_LIST);
      return res.skills ?? [];
    },
    staleTime: 60_000,
    enabled: connected,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.skills.all }),
    [queryClient],
  );

  // Invalidate on WS reconnect so post-restart dep scan results are picked up
  // even if the SKILL_DEPS_* events were emitted before the client connected.
  useEffect(() => {
    if (connected) invalidate();
  }, [connected]); // eslint-disable-line react-hooks/exhaustive-deps

  const getSkill = useCallback(
    async (name: string) => {
      if (!ws.isConnected) return null;
      return ws.call<SkillInfo & { content: string }>(Methods.SKILLS_GET, { name });
    },
    [ws],
  );

  const uploadSkill = useCallback(
    async (file: File) => {
      const formData = new FormData();
      formData.append("file", file);
      const res = await http.upload<{ id: string; slug: string; version: number; name: string }>(
        "/v1/skills/upload",
        formData,
      );
      await invalidate();
      return res;
    },
    [http, invalidate],
  );

  const updateSkill = useCallback(
    async (id: string, updates: Record<string, unknown>) => {
      const res = await http.put<{ ok: string }>(`/v1/skills/${id}`, updates);
      await invalidate();
      return res;
    },
    [http, invalidate],
  );

  const deleteSkill = useCallback(
    async (id: string) => {
      const res = await http.delete<{ ok: string }>(`/v1/skills/${id}`);
      await invalidate();
      return res;
    },
    [http, invalidate],
  );

  const getSkillVersions = useCallback(
    async (id: string) => {
      return http.get<SkillVersions>(`/v1/skills/${id}/versions`);
    },
    [http],
  );

  const getSkillFiles = useCallback(
    async (id: string, version?: number) => {
      const q = version != null ? `?version=${version}` : "";
      const res = await http.get<{ files: SkillFile[] }>(`/v1/skills/${id}/files${q}`);
      return res.files ?? [];
    },
    [http],
  );

  const getSkillFileContent = useCallback(
    async (id: string, path: string, version?: number) => {
      const q = version != null ? `?version=${version}` : "";
      return http.get<{ content: string; path: string; size: number }>(
        `/v1/skills/${id}/files/${encodeURIComponent(path)}${q}`,
      );
    },
    [http],
  );

  const rescanDeps = useCallback(
    async () => {
      const res = await http.post<{ updated: number; results: Array<{ slug: string; status: string; missing?: string[] }> }>(
        "/v1/skills/rescan-deps",
        {},
      );
      await invalidate();
      return res;
    },
    [http, invalidate],
  );

  const installDeps = useCallback(
    async () => {
      const res = await http.post<{
        system?: string[];
        pip?: string[];
        npm?: string[];
        errors?: string[];
      }>("/v1/skills/install-deps", {});
      await invalidate();
      return res;
    },
    [http, invalidate],
  );

  const installSingleDep = useCallback(
    async (dep: string) => {
      const res = await http.post<{ ok: boolean; error?: string }>("/v1/skills/install-dep", { dep });
      if (!res.ok) throw new Error(res.error ?? "install failed");
      await invalidate();
      return res;
    },
    [http, invalidate],
  );

  const toggleSkill = useCallback(
    async (id: string, enabled: boolean) => {
      const res = await http.post<{ ok: boolean; enabled: boolean; status: string }>(
        `/v1/skills/${id}/toggle`,
        { enabled },
      );
      await invalidate();
      return res;
    },
    [http, invalidate],
  );

  return {
    skills, loading, refresh: invalidate, getSkill,
    uploadSkill, updateSkill, deleteSkill,
    getSkillVersions, getSkillFiles, getSkillFileContent, rescanDeps, installDeps, installSingleDep, toggleSkill,
  };
}

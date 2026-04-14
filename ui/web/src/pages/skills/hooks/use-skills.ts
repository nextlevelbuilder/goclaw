import { useCallback, useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWs, useHttp } from "@/hooks/use-ws";
import { useAuthStore } from "@/stores/use-auth-store";
import { Methods } from "@/api/protocol";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";
import i18next from "i18next";
import { userFriendlyError } from "@/lib/error-utils";
import type { SkillInfo, SkillFile, SkillVersions } from "@/types/skill";

export type { SkillInfo, SkillFile, SkillVersions };

export type SkillUploadResponse = {
  /** Absent when status is "unchanged" */
  id?: string;
  slug: string;
  version: number;
  name: string;
  /** "active" | "unchanged" | "archived" */
  status?: string;
  is_new?: boolean;
  deps_warning?: string;
  deps_errors?: string[];
  missing_deps?: string[];
  deps_installed?: boolean;
};

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
  }, [connected]);  

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
      const res = await http.upload<SkillUploadResponse>(
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
      try {
        const res = await http.put<{ ok: string }>(`/v1/skills/${id}`, updates);
        await invalidate();
        toast.success(i18next.t("skills:toast.updated"));
        return res;
      } catch (err) {
        toast.error(i18next.t("skills:toast.updateFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [http, invalidate],
  );

  const deleteSkill = useCallback(
    async (id: string) => {
      try {
        const res = await http.delete<{ ok: string }>(`/v1/skills/${id}`);
        await invalidate();
        toast.success(i18next.t("skills:toast.deleted"));
        return res;
      } catch (err) {
        toast.error(i18next.t("skills:toast.deleteFailed"), userFriendlyError(err));
        throw err;
      }
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
      try {
        const res = await http.post<{ updated: number; results: Array<{ slug: string; status: string; missing?: string[] }> }>(
          "/v1/skills/rescan-deps",
          {},
        );
        await invalidate();
        if (res.updated > 0) {
          toast.success(i18next.t("skills:toast.rescanUpdated", { count: res.updated }));
        } else {
          toast.info(i18next.t("skills:toast.rescanNoChanges"));
        }
        return res;
      } catch (err) {
        toast.error(i18next.t("skills:toast.rescanFailed"), userFriendlyError(err));
        throw err;
      }
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

  const setTenantConfig = useCallback(
    async (id: string, enabled: boolean) => {
      try {
        await http.put(`/v1/skills/${id}/tenant-config`, { enabled });
        await invalidate();
        toast.success(i18next.t("skills:toast.updated"));
      } catch (err) {
        toast.error(i18next.t("skills:toast.updateFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [http, invalidate],
  );

  const deleteTenantConfig = useCallback(
    async (id: string) => {
      try {
        await http.delete(`/v1/skills/${id}/tenant-config`);
        await invalidate();
        toast.success(i18next.t("skills:toast.updated"));
      } catch (err) {
        toast.error(i18next.t("skills:toast.updateFailed"), userFriendlyError(err));
        throw err;
      }
    },
    [http, invalidate],
  );

  return {
    skills, loading, refresh: invalidate, getSkill,
    uploadSkill, updateSkill, deleteSkill,
    getSkillVersions, getSkillFiles, getSkillFileContent, rescanDeps, installDeps, installSingleDep, toggleSkill,
    setTenantConfig, deleteTenantConfig,
  };
}

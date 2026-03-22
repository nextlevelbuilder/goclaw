import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import i18next from "i18next";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import { toast } from "@/stores/use-toast-store";

export interface ProjectData {
  id: string;
  name: string;
  slug: string;
  channel_type?: string | null;
  chat_id?: string | null;
  team_id?: string | null;
  description?: string | null;
  status: string;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface ProjectInput {
  name: string;
  slug: string;
  channel_type?: string | null;
  chat_id?: string | null;
  description?: string | null;
  status?: string;
}

export interface ProjectMCPOverride {
  id: string;
  project_id: string;
  server_name: string;
  env_overrides: Record<string, string>;
  enabled: boolean;
}

export function useProjects() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data: projects = [], isLoading: loading } = useQuery({
    queryKey: queryKeys.projects.all,
    queryFn: async () => {
      const res = await http.get<{ projects: ProjectData[] }>("/v1/projects");
      return res.projects ?? [];
    },
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.projects.all }),
    [queryClient],
  );

  const createProject = useCallback(
    async (data: ProjectInput) => {
      try {
        const res = await http.post<ProjectData>("/v1/projects", data);
        await invalidate();
        toast.success(i18next.t("projects:toast.created"));
        return res;
      } catch (err) {
        toast.error(i18next.t("projects:toast.failedCreate"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const updateProject = useCallback(
    async (id: string, data: Partial<ProjectInput>) => {
      try {
        await http.put(`/v1/projects/${id}`, data);
        await invalidate();
        toast.success(i18next.t("projects:toast.updated"));
      } catch (err) {
        toast.error(i18next.t("projects:toast.failedUpdate"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const deleteProject = useCallback(
    async (id: string) => {
      try {
        await http.delete(`/v1/projects/${id}`);
        await invalidate();
        toast.success(i18next.t("projects:toast.deleted"));
      } catch (err) {
        toast.error(i18next.t("projects:toast.failedDelete"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http, invalidate],
  );

  const listOverrides = useCallback(
    async (projectId: string) => {
      const res = await http.get<{ overrides: ProjectMCPOverride[] }>(`/v1/projects/${projectId}/mcp`);
      return res.overrides ?? [];
    },
    [http],
  );

  const setOverride = useCallback(
    async (projectId: string, serverName: string, envOverrides: Record<string, string>) => {
      try {
        await http.put(`/v1/projects/${projectId}/mcp/${serverName}`, envOverrides);
        toast.success(i18next.t("projects:overrides.saved"));
      } catch (err) {
        toast.error(i18next.t("projects:overrides.failedSave"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http],
  );

  const removeOverride = useCallback(
    async (projectId: string, serverName: string) => {
      try {
        await http.delete(`/v1/projects/${projectId}/mcp/${serverName}`);
        toast.success(i18next.t("projects:overrides.deleted"));
      } catch (err) {
        toast.error(i18next.t("projects:overrides.failedDelete"), err instanceof Error ? err.message : "");
        throw err;
      }
    },
    [http],
  );

  return {
    projects,
    loading,
    refresh: invalidate,
    createProject,
    updateProject,
    deleteProject,
    listOverrides,
    setOverride,
    removeOverride,
  };
}

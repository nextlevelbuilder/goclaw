import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useWs, useHttp } from "@/hooks/use-ws";
import { Methods } from "@/api/protocol";
import { queryKeys } from "@/lib/query-keys";
import type { SkillInfo } from "@/types/skill";

export type { SkillInfo };

export function useSkills() {
  const ws = useWs();
  const http = useHttp();
  const queryClient = useQueryClient();

  const { data: skills = [], isLoading: loading } = useQuery({
    queryKey: queryKeys.skills.all,
    queryFn: async () => {
      if (!ws.isConnected) return [];
      const res = await ws.call<{ skills: SkillInfo[] }>(Methods.SKILLS_LIST);
      return res.skills ?? [];
    },
    staleTime: 60_000,
  });

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey: queryKeys.skills.all }),
    [queryClient],
  );

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

  const deleteSkill = useCallback(
    async (id: string) => {
      const res = await http.delete<{ ok: string }>(`/v1/skills/${id}`);
      await invalidate();
      return res;
    },
    [http, invalidate],
  );

  return { skills, loading, refresh: invalidate, getSkill, uploadSkill, deleteSkill };
}

import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { SkillWithGrant } from "@/types/skill";
import { toast } from "@/stores/use-toast-store";
import i18next from "i18next";
import { userFriendlyError } from "@/lib/error-utils";

export function useAgentSkills(agentId: string) {
  const http = useHttp();
  const queryClient = useQueryClient();
  const queryKey = queryKeys.skills.agentGrants(agentId);

  const { data: skills = [], isLoading: loading } = useQuery({
    queryKey,
    queryFn: () =>
      http
        .get<{ skills: SkillWithGrant[] }>(`/v1/agents/${agentId}/skills`)
        .then((r) => r.skills ?? []),
  });

  const optimisticToggle = useCallback(
    (skillId: string, granted: boolean) => {
      queryClient.setQueryData<SkillWithGrant[]>(queryKey, (old) =>
        old?.map((s) => (s.id === skillId ? { ...s, granted } : s)),
      );
    },
    [queryClient, queryKey],
  );

  const invalidate = useCallback(
    () => queryClient.invalidateQueries({ queryKey }),
    [queryClient, queryKey],
  );

  const grantSkill = useCallback(
    async (skillId: string) => {
      optimisticToggle(skillId, true);
      try {
        await http.post(`/v1/skills/${skillId}/grants/agent`, { agent_id: agentId });
        toast.success(i18next.t("agents:toast.skillGranted"));
      } catch (err) {
        toast.error(i18next.t("agents:toast.skillGrantFailed"), userFriendlyError(err));
        throw err;
      } finally {
        await invalidate();
      }
    },
    [http, agentId, invalidate, optimisticToggle],
  );

  const revokeSkill = useCallback(
    async (skillId: string) => {
      optimisticToggle(skillId, false);
      try {
        await http.delete(`/v1/skills/${skillId}/grants/agent/${agentId}`);
        toast.success(i18next.t("agents:toast.skillRevoked"));
      } catch (err) {
        toast.error(i18next.t("agents:toast.skillRevokeFailed"), userFriendlyError(err));
        throw err;
      } finally {
        await invalidate();
      }
    },
    [http, agentId, invalidate, optimisticToggle],
  );

  return { skills, loading, grantSkill, revokeSkill };
}

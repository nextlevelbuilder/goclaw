import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type {
  SkillActivityLog,
  SkillEvolutionSettings,
  SkillImprovementSuggestion,
  SkillUsageStats,
} from "@/types/skill";

export function useSkillEvolution(skillId: string | undefined, enabled: boolean) {
  const http = useHttp();
  const queryClient = useQueryClient();

  const settings = useQuery({
    queryKey: skillId ? queryKeys.skills.evolution(skillId) : ["skills", "missing", "evolution"],
    queryFn: () => http.get<SkillEvolutionSettings>(`/v1/skills/${skillId}/evolution`),
    enabled: enabled && !!skillId,
  });

  const metrics = useQuery({
    queryKey: skillId ? queryKeys.skills.metrics(skillId) : ["skills", "missing", "metrics"],
    queryFn: () => http.get<SkillUsageStats>(`/v1/skills/${skillId}/metrics`),
    enabled: enabled && !!skillId,
  });

  const suggestions = useQuery({
    queryKey: skillId ? queryKeys.skills.suggestions(skillId) : ["skills", "missing", "suggestions"],
    queryFn: async () => {
      const res = await http.get<{ suggestions: SkillImprovementSuggestion[] }>(`/v1/skills/${skillId}/evolution/suggestions`);
      return res.suggestions ?? [];
    },
    enabled: enabled && !!skillId,
  });

  const activity = useQuery({
    queryKey: skillId ? queryKeys.skills.activity(skillId) : ["skills", "missing", "activity"],
    queryFn: async () => {
      const res = await http.get<{ activity: SkillActivityLog[] }>(`/v1/skills/${skillId}/activity`);
      return res.activity ?? [];
    },
    enabled: enabled && !!skillId,
    retry: false,
  });

  const invalidate = useCallback(async () => {
    if (!skillId) return;
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: queryKeys.skills.evolution(skillId) }),
      queryClient.invalidateQueries({ queryKey: queryKeys.skills.metrics(skillId) }),
      queryClient.invalidateQueries({ queryKey: queryKeys.skills.suggestions(skillId) }),
      queryClient.invalidateQueries({ queryKey: queryKeys.skills.activity(skillId) }),
    ]);
  }, [queryClient, skillId]);

  const updateSettings = useCallback(async (updates: Partial<Pick<SkillEvolutionSettings, "enabled" | "mode">>) => {
    if (!skillId) return;
    await http.patch<SkillEvolutionSettings>(`/v1/skills/${skillId}/evolution`, updates);
    await invalidate();
  }, [http, invalidate, skillId]);

  const approveSuggestion = useCallback(async (id: string) => {
    if (!skillId) return;
    await http.post(`/v1/skills/${skillId}/evolution/suggestions/${id}/approve`, {});
    await invalidate();
  }, [http, invalidate, skillId]);

  const rejectSuggestion = useCallback(async (id: string) => {
    if (!skillId) return;
    await http.post(`/v1/skills/${skillId}/evolution/suggestions/${id}/reject`, {});
    await invalidate();
  }, [http, invalidate, skillId]);

  const applySuggestion = useCallback(async (id: string) => {
    if (!skillId) return;
    await http.post(`/v1/skills/${skillId}/evolution/suggestions/${id}/apply`, {});
    await invalidate();
  }, [http, invalidate, skillId]);

  return {
    settings,
    metrics,
    suggestions,
    activity,
    updateSettings,
    approveSuggestion,
    rejectSuggestion,
    applySuggestion,
  };
}

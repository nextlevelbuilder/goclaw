import { useBuilderSessions } from "@/hooks/use-builder-sessions";

export function useSkillBuilderSessions(agentKey: string) {
  return useBuilderSessions(agentKey, "skill-builder-");
}

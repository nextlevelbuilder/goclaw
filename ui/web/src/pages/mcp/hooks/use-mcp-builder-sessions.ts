import { useBuilderSessions } from "@/hooks/use-builder-sessions";

export function useMCPBuilderSessions(agentKey: string) {
  return useBuilderSessions(agentKey, "mcp-builder-");
}

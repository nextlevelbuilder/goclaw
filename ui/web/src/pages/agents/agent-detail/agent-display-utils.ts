import type { AgentData, ChatGPTOAuthRoutingConfig } from "@/types/agent";
import type { ProviderData } from "@/types/provider";

/** Matches a standard UUID v4 string. */
export const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export interface NormalizedChatGPTOAuthRouting {
  strategy: "manual" | "round_robin";
  extraProviderNames: string[];
}

/** Returns the display name for an agent, falling back to agent_key or unnamedLabel. */
export function agentDisplayName(
  agent: { display_name?: string; agent_key: string },
  unnamedLabel: string,
): string {
  if (agent.display_name) return agent.display_name;
  if (UUID_RE.test(agent.agent_key)) return unnamedLabel;
  return agent.agent_key;
}

/** Returns a shortened agent key for subtitle display (truncates UUIDs). */
export function agentKeyDisplay(agentKey: string): string {
  return UUID_RE.test(agentKey) ? agentKey.slice(0, 8) + "…" : agentKey;
}

/** Returns normalized ChatGPT OAuth routing config from agent other_config. */
export function normalizeChatGPTOAuthRouting(otherConfig?: Record<string, unknown> | null): NormalizedChatGPTOAuthRouting {
  const routing = (otherConfig?.chatgpt_oauth_routing ?? {}) as Record<string, unknown>;
  return {
    strategy: routing.strategy === "round_robin" ? "round_robin" : "manual",
    extraProviderNames: Array.isArray(routing.extra_provider_names)
      ? routing.extra_provider_names.filter((name): name is string => typeof name === "string" && name.trim().length > 0)
      : [],
  };
}

/** Returns true when an agent has active multi-account ChatGPT OAuth routing configured. */
export function hasActiveChatGPTOAuthRouting(otherConfig?: Record<string, unknown> | null): boolean {
  const routing = normalizeChatGPTOAuthRouting(otherConfig);
  return routing.strategy === "round_robin" || routing.extraProviderNames.length > 0;
}

export function buildAgentOtherConfigWithChatGPTOAuthRouting(
  agent: AgentData,
  providers: ProviderData[],
  routing: ChatGPTOAuthRoutingConfig,
): Record<string, unknown> {
  const existing = (agent.other_config as Record<string, unknown> | null) ?? {};
  const otherBase: Record<string, unknown> = { ...existing };
  const currentProvider = providers.find((provider) => provider.name === agent.provider);
  const hadRoutingConfig = typeof existing.chatgpt_oauth_routing === "object" && existing.chatgpt_oauth_routing !== null;

  delete otherBase.chatgpt_oauth_routing;
  if (
    (currentProvider?.provider_type === "chatgpt_oauth" || hadRoutingConfig)
    && (
      routing.strategy === "round_robin"
      || (routing.extra_provider_names?.length ?? 0) > 0
    )
  ) {
    otherBase.chatgpt_oauth_routing = {
      strategy: routing.strategy === "round_robin" ? "round_robin" : "manual",
      extra_provider_names: routing.extra_provider_names ?? [],
    };
  }

  return otherBase;
}

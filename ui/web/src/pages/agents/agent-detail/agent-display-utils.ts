import type {
  AgentData,
  ChatGPTOAuthRoutingConfig,
  ChatGPTOAuthRoutingOverrideMode,
  EffectiveChatGPTOAuthRoutingStrategy,
} from "@/types/agent";
import {
  getChatGPTOAuthProviderRouting,
  normalizeChatGPTOAuthStrategy,
} from "@/types/provider";
import type {
  ChatGPTOAuthQuotaFailureKind,
  ChatGPTOAuthRouteReadiness,
} from "./chatgpt-oauth-quota-utils";

/** Matches a standard UUID v4 string. */
export const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export interface NormalizedChatGPTOAuthRouting {
  isExplicit: boolean;
  overrideMode: ChatGPTOAuthRoutingOverrideMode;
  strategy: EffectiveChatGPTOAuthRoutingStrategy;
  extraProviderNames: string[];
}

export interface EffectiveChatGPTOAuthRouting {
  source: "single" | "provider_default" | "agent_custom";
  overrideMode: ChatGPTOAuthRoutingOverrideMode;
  strategy: EffectiveChatGPTOAuthRoutingStrategy;
  extraProviderNames: string[];
  poolProviderNames: string[];
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
  const raw = otherConfig?.chatgpt_oauth_routing;
  if (!raw || typeof raw !== "object") {
    return {
      isExplicit: false,
      overrideMode: "custom",
      strategy: "primary_first",
      extraProviderNames: [],
    };
  }
  const routing = raw as Record<string, unknown>;
  const hasStrategyField =
    typeof routing.strategy === "string" && routing.strategy.trim().length > 0;
  const hasExtraProviderField = Array.isArray(routing.extra_provider_names);
  const overrideMode = routing.override_mode === "inherit" ? "inherit" : "custom";
  const extraProviderNames = Array.from(
    new Set(
      (Array.isArray(routing.extra_provider_names) ? routing.extra_provider_names : [])
        .filter((name): name is string => typeof name === "string")
        .map((name) => name.trim())
        .filter(Boolean),
    ),
  );
  const strategy = normalizeChatGPTOAuthStrategy(routing.strategy);
  const isExplicit =
    routing.override_mode === "inherit" ||
    routing.override_mode === "custom" ||
    hasStrategyField ||
    hasExtraProviderField ||
    strategy !== "primary_first" ||
    extraProviderNames.length > 0;
  return {
    isExplicit,
    overrideMode,
    strategy,
    extraProviderNames,
  };
}

/** Returns true when an agent has active multi-account ChatGPT OAuth routing configured. */
export function hasActiveChatGPTOAuthRouting(otherConfig?: Record<string, unknown> | null): boolean {
  const routing = normalizeChatGPTOAuthRouting(otherConfig);
  return routing.isExplicit && (routing.strategy !== "primary_first" || routing.extraProviderNames.length > 0);
}

export function normalizeChatGPTOAuthRoutingInput(
  routing?: ChatGPTOAuthRoutingConfig | null,
): NormalizedChatGPTOAuthRouting {
  if (!routing) {
    return {
      isExplicit: false,
      overrideMode: "custom",
      strategy: "primary_first",
      extraProviderNames: [],
    };
  }
  return normalizeChatGPTOAuthRouting({
    chatgpt_oauth_routing: routing,
  });
}

export function resolveEffectiveChatGPTOAuthRouting(
  baseProviderName: string,
  providerSettings?: Record<string, unknown>,
  agentRouting?: NormalizedChatGPTOAuthRouting,
): EffectiveChatGPTOAuthRouting {
  const providerDefaults = getChatGPTOAuthProviderRouting(providerSettings);
  const normalizedAgent =
    agentRouting ??
    ({
      isExplicit: false,
      overrideMode: "custom",
      strategy: "primary_first",
      extraProviderNames: [],
    } satisfies NormalizedChatGPTOAuthRouting);

  let source: EffectiveChatGPTOAuthRouting["source"] = "single";
  let strategy: EffectiveChatGPTOAuthRoutingStrategy = normalizedAgent.strategy;
  let extraProviderNames = normalizedAgent.extraProviderNames;
  let overrideMode: ChatGPTOAuthRoutingOverrideMode = normalizedAgent.overrideMode;

  if (normalizedAgent.overrideMode === "inherit") {
    source = providerDefaults ? "provider_default" : "single";
    strategy = providerDefaults?.strategy ?? "primary_first";
    extraProviderNames = providerDefaults?.extraProviderNames ?? [];
    overrideMode = "inherit";
  } else if (normalizedAgent.isExplicit) {
    source = "agent_custom";
    overrideMode = "custom";
  } else if (providerDefaults) {
    source = "provider_default";
    strategy = providerDefaults.strategy;
    extraProviderNames = providerDefaults.extraProviderNames;
    overrideMode = "inherit";
  }

  if (
    providerDefaults?.extraProviderNames.length &&
    source === "agent_custom"
  ) {
    if (strategy === "primary_first" && extraProviderNames.length === 0) {
      extraProviderNames = [];
    } else {
      extraProviderNames = providerDefaults.extraProviderNames;
    }
  }

  return {
    source,
    overrideMode,
    strategy,
    extraProviderNames,
    poolProviderNames: Array.from(
      new Set([baseProviderName, ...extraProviderNames].filter(Boolean)),
    ),
  };
}

/** Maps strategy value to its i18n label key. */
export function strategyLabelKey(
  strategy: EffectiveChatGPTOAuthRoutingStrategy,
): string {
  if (strategy === "round_robin") return "chatgptOAuthRouting.strategy.roundRobin";
  if (strategy === "priority_order") return "chatgptOAuthRouting.strategy.priorityOrder";
  return "chatgptOAuthRouting.strategy.primaryFirst";
}

/** Maps route readiness state to badge variant. */
export function routeBadgeVariant(
  readiness: ChatGPTOAuthRouteReadiness,
): "success" | "warning" | "outline" | "destructive" {
  if (readiness === "healthy") return "success";
  if (readiness === "fallback") return "warning";
  if (readiness === "checking") return "outline";
  return "destructive";
}

/** Maps route readiness state to its i18n label key. */
export function routeLabelKey(readiness: ChatGPTOAuthRouteReadiness): string {
  if (readiness === "healthy") return "chatgptOAuthRouting.routerActiveTitle";
  if (readiness === "fallback") return "chatgptOAuthRouting.fallbackTitle";
  if (readiness === "checking") return "chatgptOAuthRouting.checkingTitle";
  return "chatgptOAuthRouting.blockedNowTitle";
}

/** Maps quota failure kind to badge variant. */
export const failureVariantByKind: Record<
  ChatGPTOAuthQuotaFailureKind,
  "destructive" | "warning" | "outline"
> = {
  billing: "destructive",
  exhausted: "destructive",
  reauth: "warning",
  forbidden: "destructive",
  needs_setup: "warning",
  retry_later: "outline",
  unavailable: "outline",
};

export function buildAgentOtherConfigWithChatGPTOAuthRouting(
  agent: AgentData,
  routing: ChatGPTOAuthRoutingConfig,
  providerSettings?: Record<string, unknown>,
): Record<string, unknown> {
  const existing = (agent.other_config as Record<string, unknown> | null) ?? {};
  const otherBase: Record<string, unknown> = { ...existing };
  const providerDefaults = getChatGPTOAuthProviderRouting(providerSettings);
  const normalized = normalizeChatGPTOAuthRoutingInput(routing);

  delete otherBase.chatgpt_oauth_routing;
  if (normalized.overrideMode === "inherit") {
    otherBase.chatgpt_oauth_routing = {
      override_mode: "inherit",
    };
    return otherBase;
  }

  if (
    providerDefaults ||
    normalized.isExplicit ||
    normalized.strategy !== "primary_first" ||
    normalized.extraProviderNames.length > 0
  ) {
    const customRouting: Record<string, unknown> = {
      override_mode: "custom",
      strategy: normalized.strategy,
    };
    if (
      !providerDefaults ||
      (normalized.strategy === "primary_first" &&
        normalized.extraProviderNames.length === 0)
    ) {
      customRouting.extra_provider_names = normalized.extraProviderNames;
    }
    otherBase.chatgpt_oauth_routing = {
      ...customRouting,
    };
  }

  return otherBase;
}

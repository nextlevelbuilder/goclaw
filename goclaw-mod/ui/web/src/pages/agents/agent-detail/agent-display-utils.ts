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

/** Reads prompt_mode from agent.other_config JSONB bag, defaults to "full". */
export function readPromptMode(agent: { other_config?: Record<string, unknown> | null }): string {
  const bag = (agent.other_config ?? {}) as Record<string, unknown>;
  return (bag.prompt_mode as string) || "full";
}

/** Matches a standard UUID v4 string. */
export const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export interface NormalizedChatGPTOAuthRouting {
  isExplicit: boolean;
  overrideMode: ChatGPTOAuthRoutingOverrideMode;
  strategy: EffectiveChatGPTOAuthRoutingStrategy;
  extraProviderNames: string[];
  hasExplicitExtraProviderNames: boolean;
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

/**
 * Returns normalized ChatGPT OAuth routing from agent top-level chatgpt_oauth_routing field.
 * Also accepts legacy other_config shape for backward compatibility during transition.
 */
export function normalizeChatGPTOAuthRouting(
  routingOrLegacy?: ChatGPTOAuthRoutingConfig | Record<string, unknown> | null,
): NormalizedChatGPTOAuthRouting {
  // Detect legacy call: Record with chatgpt_oauth_routing key (old other_config shape)
  let raw: unknown = routingOrLegacy;
  if (
    raw &&
    typeof raw === "object" &&
    !Array.isArray(raw) &&
    "chatgpt_oauth_routing" in (raw as Record<string, unknown>)
  ) {
    raw = (raw as Record<string, unknown>).chatgpt_oauth_routing;
  }
  if (!raw || typeof raw !== "object") {
    return {
      isExplicit: false,
      overrideMode: "custom",
      strategy: "priority_order",
      extraProviderNames: [],
      hasExplicitExtraProviderNames: false,
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
    strategy !== "priority_order" ||
    extraProviderNames.length > 0;
  return {
    isExplicit,
    overrideMode,
    strategy,
    extraProviderNames,
    hasExplicitExtraProviderNames: hasExtraProviderField,
  };
}

/** Returns true when an agent has active multi-account ChatGPT OAuth routing configured. */
export function hasActiveChatGPTOAuthRouting(
  routing?: ChatGPTOAuthRoutingConfig | Record<string, unknown> | null,
): boolean {
  const normalized = normalizeChatGPTOAuthRouting(routing);
  return normalized.isExplicit && (
    normalized.strategy === "round_robin" ||
    normalized.extraProviderNames.length > 0
  );
}

export function normalizeChatGPTOAuthRoutingInput(
  routing?: ChatGPTOAuthRoutingConfig | null,
): NormalizedChatGPTOAuthRouting {
  if (!routing) {
    return {
      isExplicit: false,
      overrideMode: "custom",
      strategy: "priority_order",
      extraProviderNames: [],
      hasExplicitExtraProviderNames: false,
    };
  }
  return normalizeChatGPTOAuthRouting(routing);
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
      strategy: "priority_order",
      extraProviderNames: [],
      hasExplicitExtraProviderNames: false,
    } satisfies NormalizedChatGPTOAuthRouting);

  let source: EffectiveChatGPTOAuthRouting["source"] = "single";
  let strategy: EffectiveChatGPTOAuthRoutingStrategy = normalizedAgent.strategy;
  let extraProviderNames = normalizedAgent.extraProviderNames;
  let overrideMode: ChatGPTOAuthRoutingOverrideMode = normalizedAgent.overrideMode;

  if (normalizedAgent.overrideMode === "inherit") {
    source = providerDefaults ? "provider_default" : "single";
    strategy = providerDefaults?.strategy ?? "priority_order";
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
    if (normalizedAgent.hasExplicitExtraProviderNames && extraProviderNames.length === 0) {
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
  return "chatgptOAuthRouting.strategy.priorityOrder";
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

/**
 * Builds an update payload with chatgpt_oauth_routing at top level.
 * Returns an object whose keys are meant to be spread directly into the
 * agent update request (not nested under other_config).
 */
export function buildAgentOtherConfigWithChatGPTOAuthRouting(
  agent: AgentData,
  routing: ChatGPTOAuthRoutingConfig,
  providerSettings?: Record<string, unknown>,
): Record<string, unknown> {
  const providerDefaults = getChatGPTOAuthProviderRouting(providerSettings);
  const normalized = normalizeChatGPTOAuthRoutingInput(routing);

  // Base: preserve other_config as-is (extensibility bag), set routing at top level
  const result: Record<string, unknown> = {
    other_config: agent.other_config ?? null,
  };

  if (normalized.overrideMode === "inherit") {
    result.chatgpt_oauth_routing = { override_mode: "inherit" };
    return result;
  }

  if (
    providerDefaults ||
    normalized.isExplicit ||
    normalized.extraProviderNames.length > 0
  ) {
    const customRouting: Record<string, unknown> = {
      override_mode: "custom",
      strategy: normalized.strategy,
    };
    if (normalized.hasExplicitExtraProviderNames || normalized.extraProviderNames.length > 0) {
      customRouting.extra_provider_names = normalized.extraProviderNames;
    }
    result.chatgpt_oauth_routing = customRouting;
  } else {
    result.chatgpt_oauth_routing = null;
  }

  return result;
}

import type {
  ChatGPTOAuthRoutingConfig,
  EffectiveChatGPTOAuthRoutingStrategy,
} from "./agent";

export interface ProviderData {
  id: string;
  name: string;
  display_name: string;
  provider_type: string;
  api_base: string;
  api_key: string; // masked "***" from server
  enabled: boolean;
  settings?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface ProviderInput {
  name: string;
  display_name?: string;
  provider_type: string;
  api_base?: string;
  api_key?: string;
  enabled?: boolean;
  settings?: Record<string, unknown>;
}

export interface ModelInfo {
  id: string;
  name?: string;
  reasoning?: ReasoningCapability;
}

export interface ProviderReasoningDefaults {
  effort?: string;
  fallback?: "downgrade" | "provider_default" | "off";
}

export interface ProviderModelsResponse {
  models: ModelInfo[];
  reasoning_defaults?: ProviderReasoningDefaults;
}

export interface ReasoningCapability {
  levels?: string[];
  default_effort?: string;
}

export interface EmbeddingSettings {
  enabled: boolean;
  model?: string;
  api_base?: string;
  dimensions?: number; // truncate output to N dims (e.g. 1536); 0/undefined = model default
}

export interface NormalizedChatGPTOAuthProviderRouting {
  strategy: EffectiveChatGPTOAuthRoutingStrategy;
  extraProviderNames: string[];
}

/** Extract embedding settings from provider.settings */
export function getEmbeddingSettings(settings?: Record<string, unknown>): EmbeddingSettings | null {
  if (!settings?.embedding) return null;
  return settings.embedding as EmbeddingSettings;
}

function normalizeProviderNames(names: unknown): string[] {
  if (!Array.isArray(names)) return [];
  return Array.from(
    new Set(
      names
        .filter((name): name is string => typeof name === "string")
        .map((name) => name.trim())
        .filter(Boolean),
    ),
  );
}

export function normalizeChatGPTOAuthStrategy(
  strategy: unknown,
): EffectiveChatGPTOAuthRoutingStrategy {
  if (strategy === "round_robin") return "round_robin";
  return "priority_order";
}

export function normalizeReasoningEffort(value: unknown): string {
  if (typeof value !== "string") return "";
  const normalized = value.trim().toLowerCase();
  return [
    "off", "auto", "none", "minimal", "low", "medium", "high", "xhigh",
  ].includes(normalized) ? normalized : "";
}

export function normalizeReasoningFallback(
  value: unknown,
): "downgrade" | "provider_default" | "off" {
  if (value === "provider_default" || value === "off") {
    return value;
  }
  return "downgrade";
}

/** Maps advanced reasoning effort levels to the legacy three-tier thinking_level. */
export function deriveLegacyThinkingLevel(effort: string): string {
  switch (effort) {
    case "low":
    case "medium":
    case "high":
      return effort;
    case "minimal":
      return "low";
    case "xhigh":
      return "high";
    default:
      return "off";
  }
}

export function getProviderReasoningDefaults(
  settings?: Record<string, unknown>,
): ProviderReasoningDefaults | null {
  const raw = settings?.reasoning_defaults;
  if (!raw || typeof raw !== "object") return null;
  const reasoning = raw as Record<string, unknown>;
  const effort = normalizeReasoningEffort(reasoning.effort) || "off";
  const fallback = normalizeReasoningFallback(reasoning.fallback);
  if (effort === "off" && fallback === "downgrade") {
    return null;
  }
  return { effort, fallback };
}

export function getChatGPTOAuthProviderRouting(
  settings?: Record<string, unknown>,
): NormalizedChatGPTOAuthProviderRouting | null {
  const rawPool = settings?.codex_pool;
  if (!rawPool || typeof rawPool !== "object") return null;
  const pool = rawPool as Record<string, unknown>;
  const strategy = normalizeChatGPTOAuthStrategy(pool.strategy);
  const extraProviderNames = normalizeProviderNames(pool.extra_provider_names);
  if (strategy === "priority_order" && extraProviderNames.length === 0) {
    return null;
  }
  return {
    strategy,
    extraProviderNames,
  };
}

export function buildProviderSettingsWithChatGPTOAuthRouting(
  settings: Record<string, unknown> | undefined,
  routing: ChatGPTOAuthRoutingConfig,
): Record<string, unknown> {
  const next: Record<string, unknown> = { ...(settings ?? {}) };
  const strategy = normalizeChatGPTOAuthStrategy(routing.strategy);
  const extraProviderNames = normalizeProviderNames(routing.extra_provider_names);

  delete next.codex_pool;
  if (extraProviderNames.length > 0) {
    next.codex_pool = {
      strategy,
      extra_provider_names: extraProviderNames,
    };
  }

  return next;
}

export function buildProviderSettingsWithReasoningDefaults(
  settings: Record<string, unknown> | undefined,
  reasoning: ProviderReasoningDefaults | null,
): Record<string, unknown> {
  const next: Record<string, unknown> = { ...(settings ?? {}) };
  const effort = normalizeReasoningEffort(reasoning?.effort) || "off";
  const fallback = normalizeReasoningFallback(reasoning?.fallback);

  delete next.reasoning_defaults;
  if (effort !== "off" || fallback !== "downgrade") {
    next.reasoning_defaults = {
      effort,
      fallback,
    };
  }

  return next;
}

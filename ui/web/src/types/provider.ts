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
  if (strategy === "priority_order") return "priority_order";
  return "primary_first";
}

export function getChatGPTOAuthProviderRouting(
  settings?: Record<string, unknown>,
): NormalizedChatGPTOAuthProviderRouting | null {
  const rawPool = settings?.codex_pool;
  if (!rawPool || typeof rawPool !== "object") return null;
  const pool = rawPool as Record<string, unknown>;
  const strategy = normalizeChatGPTOAuthStrategy(pool.strategy);
  const extraProviderNames = normalizeProviderNames(pool.extra_provider_names);
  if (strategy === "primary_first" && extraProviderNames.length === 0) {
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
  if (strategy !== "primary_first" || extraProviderNames.length > 0) {
    next.codex_pool = {
      strategy,
      extra_provider_names: extraProviderNames,
    };
  }

  return next;
}

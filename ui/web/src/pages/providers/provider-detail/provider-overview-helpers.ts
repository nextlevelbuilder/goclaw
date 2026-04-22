import type { ChatGPTOAuthAvailability } from "../hooks/use-chatgpt-oauth-provider-statuses";
import type { ChatGPTOAuthRoutingConfig } from "@/types/agent";
import { normalizeReasoningEffort, normalizeReasoningFallback } from "@/types/provider";

// Provider types that don't use API keys
export const NO_API_KEY_TYPES = new Set(["claude_cli", "acp", "chatgpt_oauth"]);

// Provider types that don't support embedding
export const NO_EMBEDDING_TYPES = new Set([
  "claude_cli",
  "acp",
  "chatgpt_oauth",
  "anthropic_native",
]);

export const SIMPLE_REASONING_LEVELS = new Set(["off", "low", "medium", "high"]);
export const ADVANCED_REASONING_LEVELS = ["off", "auto", "none", "minimal", "low", "medium", "high", "xhigh"] as const;
export const REASONING_FALLBACKS = ["downgrade", "provider_default", "off"] as const;

/** Determine provider OAuth availability from status map */
export function providerStatus(
  providerName: string,
  statusByName: Map<string, { availability: ChatGPTOAuthAvailability }>,
  enabled?: boolean,
): ChatGPTOAuthAvailability {
  return (
    statusByName.get(providerName)?.availability ??
    (enabled === false ? "disabled" : "needs_sign_in")
  );
}

/** Serialize routing config for dirty-check comparison */
export function routingSignature(routing: ChatGPTOAuthRoutingConfig): string {
  const extras = Array.from(
    new Set((routing.extra_provider_names ?? []).map((name) => name.trim()).filter(Boolean)),
  );
  const strategy =
    routing.strategy === "round_robin" || routing.strategy === "priority_order"
      ? routing.strategy
      : "primary_first";
  return JSON.stringify({ strategy, extra_provider_names: extras });
}

/** Serialize reasoning config for dirty-check comparison */
export function reasoningSignature(effort: string, fallback: string): string {
  return JSON.stringify({
    effort: normalizeReasoningEffort(effort) || "off",
    fallback: normalizeReasoningFallback(fallback),
  });
}

/** Return non-empty API key value for comparison (handles masked "***") */
export function comparableAPIKeyValue(
  apiKey: string,
  savedAPIKey: string,
  showApiKey: boolean,
): string {
  if (!showApiKey) return "";
  if (apiKey === "***") return "";
  if (apiKey === "" && savedAPIKey === "***") return "";
  return apiKey;
}

/** Build a serializable form signature for dirty-check comparison */
export function providerFormSignature(input: {
  displayName: string;
  apiKey: string;
  savedAPIKey: string;
  showApiKey: boolean;
  enabled: boolean;
  embEnabled: boolean;
  embModel: string;
  embApiBase: string;
  routing: ChatGPTOAuthRoutingConfig;
  reasoningEffort: string;
  reasoningFallback: string;
  isOAuth: boolean;
}): string {
  return JSON.stringify({
    displayName: input.displayName,
    apiKey: comparableAPIKeyValue(input.apiKey, input.savedAPIKey, input.showApiKey),
    enabled: input.enabled,
    embEnabled: input.embEnabled,
    embModel: input.embModel,
    embApiBase: input.embApiBase,
    routing: input.isOAuth ? routingSignature(input.routing) : "",
    reasoning: reasoningSignature(input.reasoningEffort, input.reasoningFallback),
  });
}

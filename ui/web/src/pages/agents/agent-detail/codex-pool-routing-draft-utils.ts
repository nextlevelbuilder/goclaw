import type { ChatGPTOAuthRoutingConfig } from "@/types/agent";
import {
  normalizeChatGPTOAuthRoutingInput,
  type NormalizedChatGPTOAuthRouting,
} from "./agent-display-utils";

export function buildDraftRouting(
  savedRouting: NormalizedChatGPTOAuthRouting,
): ChatGPTOAuthRoutingConfig {
  if (savedRouting.isExplicit) {
    return {
      override_mode: savedRouting.overrideMode,
      strategy: savedRouting.strategy,
      extra_provider_names: savedRouting.extraProviderNames,
    };
  }

  return {
    override_mode: "inherit",
    strategy: "primary_first",
    extra_provider_names: [],
  };
}

export function routingDraftSignature(
  routing: ChatGPTOAuthRoutingConfig,
): string {
  const normalized = normalizeChatGPTOAuthRoutingInput(routing);
  if (normalized.overrideMode === "inherit") {
    return JSON.stringify({ override_mode: "inherit" });
  }
  return JSON.stringify({
    override_mode: "custom",
    strategy: normalized.strategy,
    extra_provider_names: normalized.extraProviderNames,
  });
}

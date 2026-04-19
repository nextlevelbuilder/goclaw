import type {
  AgentData,
  ChatGPTOAuthRoutingConfig,
  CompactionConfig,
  ContextPruningConfig,
  ReasoningOverrideMode,
  SandboxConfig,
  WorkspaceSharingConfig,
} from "@/types/agent";
import {
  getProviderReasoningDefaults,
  normalizeReasoningEffort,
  normalizeReasoningFallback,
  deriveLegacyThinkingLevel,
} from "@/types/provider";
import {
  buildAgentOtherConfigWithChatGPTOAuthRouting,
  normalizeChatGPTOAuthRouting,
} from "./agent-display-utils";
import { buildDraftRouting } from "./codex-pool-routing-draft-utils";
import type { ProviderData } from "@/pages/providers/hooks/use-providers";

const SIMPLE_REASONING_LEVELS = new Set(["off", "low", "medium", "high"]);

export interface AdvancedDialogState {
  reasoningMode: ReasoningOverrideMode;
  thinkingLevel: string;
  reasoningEffort: string;
  reasoningFallback: string;
  reasoningExpert: boolean;
  chatgptRouting: ChatGPTOAuthRoutingConfig;
  wsSharing: WorkspaceSharingConfig;
  comp: CompactionConfig;
  pruneEnabled: boolean;
  prune: ContextPruningConfig;
  sbEnabled: boolean;
  sb: SandboxConfig;
}

export function deriveState(
  agent: AgentData,
  currentProvider: ProviderData | undefined,
): AdvancedDialogState {
  const providerReasoningDefaults = getProviderReasoningDefaults(currentProvider?.settings);

  // Read reasoning from top-level reasoning_config, with fallback to other_config for transition
  const reasoningCfg = (
    agent.reasoning_config ??
    (agent.other_config as Record<string, unknown> | null)?.reasoning ??
    {}
  ) as Record<string, unknown>;
  const rawThinkingLevel = normalizeReasoningEffort(
    agent.thinking_level ?? (agent.other_config as Record<string, unknown> | null)?.thinking_level,
  );
  const hasReasoningObject =
    Boolean(agent.reasoning_config) ||
    Boolean((agent.other_config as Record<string, unknown> | null)?.reasoning);
  const reasoningMode: ReasoningOverrideMode =
    reasoningCfg.override_mode === "inherit"
      ? "inherit"
      : hasReasoningObject || rawThinkingLevel
        ? "custom"
        : "inherit";
  const reasoningEffort =
    normalizeReasoningEffort(reasoningCfg.effort) ||
    rawThinkingLevel ||
    providerReasoningDefaults?.effort ||
    "off";
  const reasoningFallback = normalizeReasoningFallback(reasoningCfg.fallback);

  // Read routing from top-level field, fallback to other_config for transition
  const routing = normalizeChatGPTOAuthRouting(
    agent.chatgpt_oauth_routing ?? agent.other_config,
  );
  const draftRouting = buildDraftRouting(routing);

  return {
    reasoningMode,
    thinkingLevel: SIMPLE_REASONING_LEVELS.has(reasoningEffort)
      ? reasoningEffort
      : deriveLegacyThinkingLevel(reasoningEffort),
    reasoningEffort,
    reasoningFallback:
      reasoningMode === "inherit"
        ? (providerReasoningDefaults?.fallback ?? "downgrade")
        : reasoningFallback,
    reasoningExpert:
      reasoningMode === "custom" &&
      (hasReasoningObject ||
        !SIMPLE_REASONING_LEVELS.has(reasoningEffort) ||
        reasoningFallback !== "downgrade"),
    chatgptRouting: draftRouting,
    // Read workspace_sharing from top-level, fallback to other_config for transition
    wsSharing: (
      agent.workspace_sharing ??
      (agent.other_config as Record<string, unknown> | null)?.workspace_sharing ??
      {}
    ) as WorkspaceSharingConfig,
    comp: agent.compaction_config ?? {},
    pruneEnabled: agent.context_pruning?.mode === "cache-ttl",
    prune: agent.context_pruning ?? {},
    sbEnabled: agent.sandbox_config != null,
    sb: agent.sandbox_config ?? {},
  };
}

export interface BuildAdvancedUpdatePayloadParams {
  agent: AgentData;
  currentProvider: ProviderData | undefined;
  providersLoading: boolean;
  providerModelsLoading: boolean;
  expertReasoningAvailable: boolean;
  reasoningMode: ReasoningOverrideMode;
  reasoningEffort: string;
  reasoningExpert: boolean;
  reasoningFallback: string;
  thinkingLevel: string;
  chatgptRouting: ChatGPTOAuthRoutingConfig;
  wsSharing: WorkspaceSharingConfig;
  comp: CompactionConfig;
  pruneEnabled: boolean;
  prune: ContextPruningConfig;
  sbEnabled: boolean;
  sb: SandboxConfig;
}

export function buildAdvancedUpdatePayload(
  params: BuildAdvancedUpdatePayloadParams,
): Record<string, unknown> {
  const {
    agent, currentProvider, providersLoading, providerModelsLoading,
    expertReasoningAvailable, reasoningMode, reasoningEffort, reasoningExpert,
    reasoningFallback, thinkingLevel, chatgptRouting, wsSharing,
    comp, pruneEnabled, prune, sbEnabled, sb,
  } = params;

  const routingPayload = buildAgentOtherConfigWithChatGPTOAuthRouting(
    agent,
    chatgptRouting,
    currentProvider?.settings,
  );

  const capabilityResolutionPending =
    !currentProvider || providersLoading || providerModelsLoading;

  const updates: Record<string, unknown> = {
    compaction_config: comp,
    context_pruning: pruneEnabled
      ? { mode: "cache-ttl", ...prune }
      : { mode: "off" },
    sandbox_config: sbEnabled ? sb : null,
    ...routingPayload,
  };

  // Build reasoning_config and thinking_level as top-level fields
  if (reasoningMode === "inherit") {
    updates.reasoning_config = { override_mode: "inherit" };
    updates.thinking_level = null;
  } else {
    const shouldPersistExpertReasoning =
      reasoningExpert && (expertReasoningAvailable || capabilityResolutionPending);
    const requestedEffort = shouldPersistExpertReasoning ? reasoningEffort : thinkingLevel;
    const legacyThinkingLevel = deriveLegacyThinkingLevel(requestedEffort);
    updates.thinking_level = legacyThinkingLevel !== "off" ? legacyThinkingLevel : null;
    const reasoningConfig: Record<string, unknown> = {
      override_mode: "custom",
      effort: requestedEffort,
    };
    if (reasoningFallback !== "downgrade") reasoningConfig.fallback = reasoningFallback;
    updates.reasoning_config = reasoningConfig;
  }

  // workspace_sharing at top level
  if (
    wsSharing.shared_dm ||
    wsSharing.shared_group ||
    (wsSharing.shared_users?.length ?? 0) > 0 ||
    wsSharing.share_memory ||
    wsSharing.share_knowledge_graph ||
    wsSharing.share_sessions
  ) {
    updates.workspace_sharing = wsSharing;
  } else {
    updates.workspace_sharing = null;
  }

  return updates;
}

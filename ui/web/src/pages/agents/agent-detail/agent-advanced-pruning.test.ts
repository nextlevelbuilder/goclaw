import { describe, expect, it } from "vitest";
import type { AgentData } from "@/types/agent";
import { buildAdvancedUpdatePayload, deriveState } from "./agent-advanced-state-utils";

const baseAgent = {
  id: "agent-1",
  agent_key: "captain",
  provider: "9router",
  model: "cx/gpt-6-astra-high",
  other_config: {},
} as unknown as AgentData;

function pruneEnabledFor(contextPruning: unknown) {
  return deriveState({ ...baseAgent, context_pruning: contextPruning } as AgentData, undefined).pruneEnabled;
}

describe("context pruning toggle state", () => {
  it("is enabled when the agent inherits the backend default", () => {
    expect(pruneEnabledFor(null)).toBe(true);
    expect(pruneEnabledFor(undefined)).toBe(true);
    expect(pruneEnabledFor({ keepLastAssistants: 5 })).toBe(true);
  });

  it("is enabled for cache-ttl mode", () => {
    expect(pruneEnabledFor({ mode: "cache-ttl" })).toBe(true);
  });

  it("is disabled only for an explicit off mode", () => {
    expect(pruneEnabledFor({ mode: "off" })).toBe(false);
  });
});

function pruningPayloadFor(contextPruning: unknown, pruneEnabled: boolean, prune: Record<string, unknown>) {
  return buildAdvancedUpdatePayload({
    agent: { ...baseAgent, context_pruning: contextPruning } as AgentData,
    currentProvider: undefined,
    providersLoading: false,
    providerModelsLoading: false,
    expertReasoningAvailable: false,
    reasoningMode: "inherit",
    reasoningEffort: "off",
    reasoningExpert: false,
    reasoningFallback: "downgrade",
    thinkingLevel: "off",
    chatgptRouting: {},
    modelFallback: { enabled: false, strategy: "priority_order", candidates: [] },
    wsSharing: {},
    comp: {},
    deliveryBehaviorMode: "inherit",
    deliveryBehavior: {},
    inboundDebounceMode: "inherit",
    inboundDebounceMs: 0,
    pruneEnabled,
    prune,
    sbEnabled: false,
    sb: {},
  }).context_pruning;
}

describe("context pruning update payload", () => {
  it("leaves an inherited (unset) config unset when the user did not change it", () => {
    expect(pruningPayloadFor(null, true, {})).toBeUndefined();
  });

  it("re-enables a previously disabled agent", () => {
    expect(pruningPayloadFor({ mode: "off", keepLastAssistants: 5 }, true, { mode: "off", keepLastAssistants: 5 })).toEqual({
      mode: "cache-ttl",
      keepLastAssistants: 5,
    });
  });

  it("saves an explicit off mode when disabled", () => {
    expect(pruningPayloadFor(null, false, {})).toEqual({ mode: "off" });
  });
});

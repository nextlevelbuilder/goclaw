import { describe, expect, it } from "vitest";
import type { AgentData, DeliveryBehaviorConfig } from "@/types/agent";
import { buildAdvancedUpdatePayload } from "./agent-advanced-state-utils";

function buildPayload(deliveryBehaviorMode: "inherit" | "custom", deliveryBehavior: DeliveryBehaviorConfig) {
  return buildAdvancedUpdatePayload({
    agent: {
      id: "agent-1",
      agent_key: "support",
      provider: "openai",
      model: "gpt-4.1-mini",
      other_config: {
        keep_me: true,
        delivery_behavior: {
          quick_ack: { enabled: false },
        },
      },
    } as unknown as AgentData,
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
    deliveryBehaviorMode,
    deliveryBehavior,
    inboundDebounceMode: "inherit",
    inboundDebounceMs: 0,
    pruneEnabled: false,
    prune: {},
    sbEnabled: false,
    sb: {},
  });
}

describe("agent advanced delivery behavior payload", () => {
  it("saves custom delivery behavior without overwriting unrelated other_config", () => {
    const deliveryBehavior: DeliveryBehaviorConfig = {
      enabled: true,
      intermediate_replies: { enabled: true, mode: "sidecar_generated", provider: "groq", model: "llama-progress", timeout_ms: 5000 },
      quick_ack: { enabled: true, mode: "sidecar_generated", provider: "groq", model: "llama-ack", timeout_ms: 3500 },
    };

    const payload = buildPayload("custom", deliveryBehavior);

    expect(payload.other_config).toMatchObject({
      keep_me: true,
      delivery_behavior: deliveryBehavior,
    });
  });

  it("removes only delivery_behavior when inheriting", () => {
    const payload = buildPayload("inherit", {});

    expect(payload.other_config).toEqual({ keep_me: true });
  });
});

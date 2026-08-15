import { describe, expect, it } from "vitest";
import { buildBehaviorPatch } from "./behavior-section";

describe("buildBehaviorPatch", () => {
  it("does not include deprecated session scoping config", () => {
    const patch = buildBehaviorPatch({
      rate: {
        max_message_chars: 12000,
        rate_limit_rpm: 30,
        inbound_debounce_ms: 250,
      },
      security: {
        injection_action: "warn",
        scrub_credentials: true,
      },
      ux: {
        intent_classify: true,
        team_work_classify: false,
      },
      pendingCompaction: {
        threshold: 200,
        keep_recent: 40,
      },
      chatBehavior: {
        enabled: true,
        quick_ack: {
          enabled: true,
          mode: "sidecar_generated",
          min_delay_ms: 1000,
          provider: "",
          model: "",
          timeout_ms: 2500,
          max_tokens: 40,
          max_chars: 120,
          templates: ["Got it. Working on it..."],
        },
        intermediate_replies: {
          enabled: false,
          mode: "sidecar_generated",
          provider: "",
          model: "",
          timeout_ms: 2500,
          max_tokens: 60,
          max_chars: 180,
        },
        final_split: {
          enabled: false,
          min_chars: 1200,
          max_messages: 3,
          delay_ms: 500,
        },
      },
    });

    expect(Object.prototype.hasOwnProperty.call(patch, "sessions")).toBe(false);
    expect(patch).toMatchObject({
      gateway: {
        max_message_chars: 12000,
        rate_limit_rpm: 30,
        inbound_debounce_ms: 250,
        injection_action: "warn",
        team_work_classify: false,
      },
      agents: {
        defaults: { intent_classify: true },
      },
      tools: { scrub_credentials: true },
      channels: {
        pending_compaction: {
          threshold: 200,
          keep_recent: 40,
        },
      },
    });
  });
});

import { describe, expect, it } from "vitest";
import { buildAdvancedConfigUpdate, deriveAdvancedInitialValues } from "./channel-advanced-config";

describe("channel advanced config payload", () => {
  it("removes a stale allow_from entry when the allowed users picker is cleared", () => {
    const existing = {
      dm_policy: "pairing",
      group_policy: "open",
      allow_from: ["195835936795841454"],
      groups: {
        "-100123": { allow_from: ["group-user"] },
      },
    };

    const values = deriveAdvancedInitialValues(existing);
    const next = buildAdvancedConfigUpdate(existing, { ...values, allow_from: undefined });

    expect(next).not.toHaveProperty("allow_from");
    expect(next.dm_policy).toBe("pairing");
    expect(next.group_policy).toBe("open");
    expect(next.groups).toEqual(existing.groups);
  });

  it("persists an explicit empty allow_from array when a tags field emits one", () => {
    const existing = {
      allow_from: ["195835936795841454"],
      proxy: "http://localhost:8080",
    };

    const next = buildAdvancedConfigUpdate(existing, {
      allow_from: [],
      proxy: "",
    });

    expect(next.allow_from).toEqual([]);
    expect(next).not.toHaveProperty("proxy");
  });

  it("flattens nested delivery config for editing and writes it back nested", () => {
    const existing = {
      chat_behavior: {
        final_split: {
          enabled: "inherit",
        },
      },
    };

    const values = deriveAdvancedInitialValues(existing);
    expect(values["chat_behavior.final_split.enabled"]).toBe("inherit");

    const next = buildAdvancedConfigUpdate(existing, {
      ...values,
      "chat_behavior.final_split.enabled": "off",
    });

    expect(next).toEqual({
      chat_behavior: {
        final_split: {
          enabled: "off",
        },
      },
    });
  });
});

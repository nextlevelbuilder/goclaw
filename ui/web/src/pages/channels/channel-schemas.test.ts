import { describe, it, expect } from "vitest";
import { configSchema } from "./channel-schemas";

describe("pancake configSchema", () => {
  const pancakeConfig = configSchema["pancake"]!;

  it("has a platform field", () => {
    expect(pancakeConfig).toBeDefined();
    const platformField = pancakeConfig.find((f) => f.key === "platform");
    expect(platformField).toBeDefined();
  });

  it("platform field is type select", () => {
    const platformField = pancakeConfig.find((f) => f.key === "platform")!;
    expect(platformField.type).toBe("select");
  });

  it("platform field is required", () => {
    const platformField = pancakeConfig.find((f) => f.key === "platform")!;
    expect(platformField.required).toBe(true);
  });

  it("platform options include all expected platforms", () => {
    const platformField = pancakeConfig.find((f) => f.key === "platform")!;
    const values = platformField.options!.map((o) => o.value);
    expect(values).toContain("facebook");
    expect(values).toContain("instagram");
    expect(values).toContain("tiktok");
    expect(values).toContain("line");
    expect(values).toContain("shopee");
    expect(values).toContain("lazada");
    expect(values).toContain("tokopedia");
  });

  it("platform options do NOT include natively-supported channels", () => {
    const platformField = pancakeConfig.find((f) => f.key === "platform")!;
    const values = platformField.options!.map((o) => o.value);
    expect(values).not.toContain("telegram");
    expect(values).not.toContain("zalo");
    expect(values).not.toContain("whatsapp");
    expect(values).not.toContain("zalo_oa");
  });

  it("exposes private_reply feature toggle gated on fb/ig only", () => {
    const feat = pancakeConfig.find((f) => f.key === "features.private_reply");
    expect(feat).toBeDefined();
    expect(feat!.type).toBe("boolean");
    expect(feat!.defaultValue).toBe(false);
    expect(feat!.showWhen).toMatchObject({
      key: "platform",
      value: ["facebook", "instagram"],
    });
  });

  it("exposes private_reply_message gated by the feature toggle", () => {
    const msg = pancakeConfig.find((f) => f.key === "private_reply_message");
    expect(msg).toBeDefined();
    expect(msg!.type).toBe("textarea");
    expect(msg!.showWhen).toEqual({ key: "features.private_reply", value: "true" });
  });

  it("does NOT expose removed private_reply config fields", () => {
    const removed = [
      "private_reply_mode",
      "private_reply_only",
      "private_reply_ttl_days",
      "private_reply_options.allow_post_ids",
      "private_reply_options.deny_post_ids",
    ];
    for (const key of removed) {
      expect(pancakeConfig.find((f) => f.key === key), `field ${key} should be removed`).toBeUndefined();
    }
  });

  it("has features.auto_react boolean toggle gated on platform=facebook", () => {
    const f = pancakeConfig.find((x) => x.key === "features.auto_react");
    expect(f).toBeDefined();
    expect(f!.type).toBe("boolean");
    expect(f!.defaultValue).toBe(false);
    expect(f!.showWhen).toEqual({ key: "platform", value: "facebook" });
  });

  it.each([
    "auto_react_options.allow_post_ids",
    "auto_react_options.deny_post_ids",
    "auto_react_options.allow_user_ids",
    "auto_react_options.deny_user_ids",
  ])("has %s as tags field gated by features.auto_react", (key) => {
    const f = pancakeConfig.find((x) => x.key === key);
    expect(f).toBeDefined();
    expect(f!.type).toBe("tags");
    expect(f!.showWhen).toEqual({ key: "features.auto_react", value: "true" });
  });
});

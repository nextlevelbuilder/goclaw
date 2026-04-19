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

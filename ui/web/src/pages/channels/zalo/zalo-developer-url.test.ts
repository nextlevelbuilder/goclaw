import { describe, expect, it } from "vitest";
import { zaloWebhookSettingsURL } from "./zalo-developer-url";

describe("zaloWebhookSettingsURL", () => {
  it("opens the app-specific webhook settings route", () => {
    expect(zaloWebhookSettingsURL("3274591538521654651")).toBe(
      "https://developers.zalo.me/app/3274591538521654651/webhook",
    );
  });

  it("URL-encodes the app id and rejects unavailable values", () => {
    expect(zaloWebhookSettingsURL("{appid}")).toBe(
      "https://developers.zalo.me/app/%7Bappid%7D/webhook",
    );
    expect(zaloWebhookSettingsURL("***")).toBe("");
    expect(zaloWebhookSettingsURL(undefined)).toBe("");
  });
});

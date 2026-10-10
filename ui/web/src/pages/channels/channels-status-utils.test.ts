import { describe, expect, it } from "vitest";
import type { ChannelRuntimeStatus } from "@/types/channel";
import {
  channelTypeLabels,
  getChannelStatusFallback,
  shouldShowChannelDiagnosticsCard,
} from "./channels-status-utils";

function status(overrides: Partial<ChannelRuntimeStatus>): ChannelRuntimeStatus {
  return {
    enabled: true,
    running: true,
    state: "healthy",
    ...overrides,
  };
}

describe("shouldShowChannelDiagnosticsCard", () => {
  it("ignores Go zero-value first_failed_at timestamps on healthy channels", () => {
    expect(
      shouldShowChannelDiagnosticsCard(
        status({ first_failed_at: "0001-01-01T00:00:00Z" }),
      ),
    ).toBe(false);
  });

  it("shows diagnostics for meaningful failure timestamps", () => {
    expect(
      shouldShowChannelDiagnosticsCard(
        status({ first_failed_at: "2026-01-01T00:00:00Z" }),
      ),
    ).toBe(true);
  });

  it("still shows diagnostics for active degraded or failed states", () => {
    expect(shouldShowChannelDiagnosticsCard(status({ state: "degraded" }))).toBe(true);
    expect(shouldShowChannelDiagnosticsCard(status({ state: "failed" }))).toBe(true);
  });
});

describe("zalo_bot status utils mapping", () => {
  it("maps zalo_bot to human-readable label 'Zalo Bot'", () => {
    expect(channelTypeLabels["zalo_bot"]).toBe("Zalo Bot");
  });

  it("derives missingCredentials fallback when zalo_bot instance is enabled without credentials", () => {
    const fallback = getChannelStatusFallback({
      enabled: true,
      has_credentials: false,
      channel_type: "zalo_bot",
    });

    expect(fallback).not.toBeNull();
    expect(fallback?.failure_kind).toBe("config");
    expect(fallback?.remediation?.code).toBe("open_credentials");
    expect(fallback?.remediation?.target).toBe("credentials");
  });
});

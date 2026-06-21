import { describe, expect, it } from "vitest";
import type { ChannelRuntimeStatus } from "@/types/channel";
import { shouldShowChannelDiagnosticsCard } from "./channels-status-utils";

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

import { describe, expect, it } from "vitest";
import fixture from "./__fixtures__/provider-health.json";
import {
  cooldownRemainingMinutes,
  cooldownRemainingMs,
  healthRefetchDelay,
  parseProviderHealth,
} from "./use-provider-health";

describe("parseProviderHealth", () => {
  it("parses the real handler fixture", () => {
    const h = parseProviderHealth(fixture);
    expect(h).toEqual({
      provider_id: "0199a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b",
      provider: "dashscope",
      enabled: true,
      consecutive_failures: 3,
      cooling_down: true,
      cooldown_until: "2026-11-06T04:42:00Z",
      last_error_class: "rate_limit",
      error_counts: { rate_limit: 14, timeout: 2 },
      last_probe_at: null,
      updated_at: "2026-11-06T04:40:30Z",
      max_cooldown_seconds: 3600,
    });
  });

  it("degrades malformed fields to safe defaults", () => {
    const h = parseProviderHealth({
      cooling_down: "yes",
      cooldown_until: "not-a-date",
      error_counts: { rate_limit: "many", timeout: 1 },
      consecutive_failures: Number.NaN,
    });
    expect(h.cooling_down).toBe(false);
    expect(h.cooldown_until).toBeNull();
    expect(h.error_counts).toEqual({ timeout: 1 });
    expect(h.consecutive_failures).toBe(0);
    expect(h.last_error_class).toBe("");
  });

  it("rejects non-object payloads", () => {
    expect(() => parseProviderHealth(null)).toThrow();
    expect(() => parseProviderHealth([])).toThrow();
  });
});

describe("cooldown timing", () => {
  const health = parseProviderHealth(fixture);
  const until = Date.parse("2026-11-06T04:42:00Z");

  it("computes remaining time and rounds minutes up", () => {
    const now = until - 3.5 * 60_000;
    expect(cooldownRemainingMs(health, now)).toBe(210_000);
    expect(cooldownRemainingMinutes(health, now)).toBe(4);
  });

  it("clamps to zero once cooldown_until has passed", () => {
    expect(cooldownRemainingMs(health, until + 5_000)).toBe(0);
    expect(cooldownRemainingMinutes(health, until + 5_000)).toBe(0);
  });

  it("returns null when the provider is healthy", () => {
    const healthy = { ...health, cooling_down: false };
    expect(cooldownRemainingMs(healthy, until)).toBeNull();
    expect(cooldownRemainingMinutes(undefined, until)).toBeNull();
  });

  it("schedules a refetch right after cooldown_until, capped by the poll interval", () => {
    expect(healthRefetchDelay(health, until - 10_000)).toBe(11_500);
    expect(healthRefetchDelay(health, until - 10 * 60_000)).toBe(30_000);
    expect(healthRefetchDelay({ ...health, cooling_down: false }, until)).toBe(30_000);
    expect(healthRefetchDelay(undefined, until)).toBe(30_000);
  });

  it("floors the delay at 5s once cooldown_until has passed but the gateway still reports cooling_down", () => {
    expect(healthRefetchDelay(health, until)).toBe(5_000);
    expect(healthRefetchDelay(health, until + 60_000)).toBe(5_000);
    // A few ms left still waits for the grace period, not the floor.
    expect(healthRefetchDelay(health, until - 1)).toBe(1_501);
  });
});

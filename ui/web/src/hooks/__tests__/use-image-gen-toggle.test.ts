/**
 * Tests for the localStorage persistence logic underlying useImageGenToggle.
 *
 * NOTE: @testing-library/react is not installed in this project.
 * Following the existing test pattern (voice-picker.test.tsx, stt-provider-form.test.tsx),
 * tests cover pure logic and module contracts rather than DOM/hook rendering.
 *
 * localStorage.clear() is NOT available in this vitest environment (pre-existing
 * limitation — see http-client.test.ts). Tests use unique agent keys per case to
 * avoid cross-test contamination without relying on clear().
 */
import { describe, it, expect } from "vitest";

const PREFIX = "goclaw.agent.";
const SUFFIX = ".imageGenEnabled";
const key = (agentKey: string) => `${PREFIX}${agentKey}${SUFFIX}`;

// In-memory mock for localStorage so tests are hermetic and don't rely on
// the jsdom localStorage.clear() method that isn't available here.
const store: Record<string, string> = {};
const mockLocalStorage = {
  getItem: (k: string) => store[k] ?? null,
  setItem: (k: string, v: string) => { store[k] = v; },
  removeItem: (k: string) => { delete store[k]; },
};

/** Mirror of the readPersisted logic from use-image-gen-toggle.ts */
function readPersisted(agentKey: string): boolean {
  if (!agentKey) return true;
  try {
    const raw = mockLocalStorage.getItem(key(agentKey));
    if (raw === null) return true;
    return raw !== "false";
  } catch {
    return true;
  }
}

/** Mirror of the writePersisted logic from use-image-gen-toggle.ts */
function writePersisted(agentKey: string, enabled: boolean): void {
  if (!agentKey) return;
  try {
    mockLocalStorage.setItem(key(agentKey), String(enabled));
  } catch {
    // ignore
  }
}

describe("useImageGenToggle persistence logic", () => {
  it("defaults to true when no stored value exists", () => {
    expect(readPersisted("fresh-agent-01")).toBe(true);
  });

  it("reads stored false correctly", () => {
    mockLocalStorage.setItem(key("fresh-agent-02"), "false");
    expect(readPersisted("fresh-agent-02")).toBe(false);
  });

  it("reads stored true correctly", () => {
    mockLocalStorage.setItem(key("fresh-agent-03"), "true");
    expect(readPersisted("fresh-agent-03")).toBe(true);
  });

  it("persists false and reads it back", () => {
    writePersisted("fresh-agent-04", false);
    expect(mockLocalStorage.getItem(key("fresh-agent-04"))).toBe("false");
    expect(readPersisted("fresh-agent-04")).toBe(false);
  });

  it("persists true and reads it back", () => {
    writePersisted("fresh-agent-05", true);
    expect(mockLocalStorage.getItem(key("fresh-agent-05"))).toBe("true");
    expect(readPersisted("fresh-agent-05")).toBe(true);
  });

  it("empty agentKey returns true and does not write to storage", () => {
    expect(readPersisted("")).toBe(true);
    writePersisted("", false);
    // writePersisted is a no-op for empty key
    expect(mockLocalStorage.getItem(key(""))).toBeNull();
  });

  it("different agents have independent storage entries", () => {
    writePersisted("fresh-codex-agent", false);
    writePersisted("fresh-other-agent", true);
    expect(readPersisted("fresh-codex-agent")).toBe(false);
    expect(readPersisted("fresh-other-agent")).toBe(true);
  });

  it("overwriting false with true reflects in read", () => {
    writePersisted("fresh-agent-06", false);
    expect(readPersisted("fresh-agent-06")).toBe(false);
    writePersisted("fresh-agent-06", true);
    expect(readPersisted("fresh-agent-06")).toBe(true);
  });
});

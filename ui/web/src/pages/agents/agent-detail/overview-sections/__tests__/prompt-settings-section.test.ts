/**
 * Pure-logic tests for prompt-settings-section.tsx exported helpers.
 *
 * No DOM rendering — follows the voice-picker.test.tsx / stt-provider-form.test.tsx
 * convention: test module contracts without @testing-library/react.
 */
import { describe, it, expect } from "vitest";
import { shouldRenderTTSSection, getModelOptions } from "../prompt-settings-section";

describe("shouldRenderTTSSection", () => {
  it("returns false when provider is empty string", () => {
    expect(shouldRenderTTSSection({ provider: "" })).toBe(false);
  });

  it("returns false when provider key is absent", () => {
    expect(shouldRenderTTSSection({})).toBe(false);
  });

  it("returns false when provider is undefined", () => {
    expect(shouldRenderTTSSection({ provider: undefined })).toBe(false);
  });

  it("returns true when provider is a non-empty string", () => {
    expect(shouldRenderTTSSection({ provider: "openai" })).toBe(true);
    expect(shouldRenderTTSSection({ provider: "elevenlabs" })).toBe(true);
    expect(shouldRenderTTSSection({ provider: "edge" })).toBe(true);
    expect(shouldRenderTTSSection({ provider: "minimax" })).toBe(true);
  });
});

describe("getModelOptions", () => {
  it("returns OpenAI models for provider=openai", () => {
    const ids = getModelOptions("openai").map((m) => m.value);
    expect(ids).toContain("gpt-4o-mini-tts");
    expect(ids).toContain("tts-1");
    expect(ids).toContain("tts-1-hd");
  });

  it("returns ElevenLabs 4-model allowlist for provider=elevenlabs", () => {
    const ids = getModelOptions("elevenlabs").map((m) => m.value);
    expect(ids).toEqual(
      expect.arrayContaining([
        "eleven_v3",
        "eleven_flash_v2_5",
        "eleven_multilingual_v2",
        "eleven_turbo_v2_5",
      ]),
    );
    expect(ids).toHaveLength(4);
  });

  it("returns empty array for edge (no model concept)", () => {
    expect(getModelOptions("edge")).toEqual([]);
  });

  it("returns MiniMax models for provider=minimax", () => {
    const ids = getModelOptions("minimax").map((m) => m.value);
    expect(ids).toContain("speech-02-hd");
    expect(ids).toContain("speech-02-turbo");
  });

  it("returns empty array for unknown provider", () => {
    expect(getModelOptions("unknown_provider")).toEqual([]);
    expect(getModelOptions("")).toEqual([]);
  });
});

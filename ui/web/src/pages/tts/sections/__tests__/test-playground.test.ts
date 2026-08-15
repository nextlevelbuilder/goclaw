/**
 * Pure-logic tests for test-playground.tsx exported helpers.
 *
 * No DOM rendering — follows the voice-picker.test.tsx / stt-provider-form.test.tsx
 * convention: test module contracts without @testing-library/react.
 */
import { describe, it, expect } from "vitest";
import { canPlay, buildSynthesizeRequest } from "../test-playground";

describe("canPlay", () => {
  it("returns false when text is empty", () => {
    expect(canPlay({ text: "", provider: "openai" })).toBe(false);
  });

  it("returns false when text is whitespace-only", () => {
    expect(canPlay({ text: "   ", provider: "openai" })).toBe(false);
  });

  it("returns false when provider is empty string", () => {
    expect(canPlay({ text: "hello", provider: "" })).toBe(false);
  });

  it("returns false when text exceeds 500 chars", () => {
    expect(canPlay({ text: "a".repeat(501), provider: "openai" })).toBe(false);
  });

  it("returns true for valid text at exactly 500 chars", () => {
    expect(canPlay({ text: "a".repeat(500), provider: "openai" })).toBe(true);
  });

  it("returns true for valid text and provider", () => {
    expect(canPlay({ text: "hello", provider: "openai" })).toBe(true);
    expect(canPlay({ text: "hello", provider: "edge" })).toBe(true);
    expect(canPlay({ text: "hello", provider: "elevenlabs" })).toBe(true);
  });
});

describe("buildSynthesizeRequest", () => {
  it("includes required text and provider fields", () => {
    const r = buildSynthesizeRequest({ text: "hello", provider: "edge" });
    expect(r.text).toBe("hello");
    expect(r.provider).toBe("edge");
  });

  it("omits voice_id and model_id when not provided", () => {
    const r = buildSynthesizeRequest({ text: "hello", provider: "edge" });
    expect(r).toEqual({ text: "hello", provider: "edge" });
    expect("voice_id" in r).toBe(false);
    expect("model_id" in r).toBe(false);
  });

  it("omits voice_id when voiceId is empty string", () => {
    const r = buildSynthesizeRequest({ text: "hello", provider: "edge", voiceId: "" });
    expect("voice_id" in r).toBe(false);
  });

  it("omits model_id when modelId is empty string", () => {
    const r = buildSynthesizeRequest({ text: "hello", provider: "openai", modelId: "" });
    expect("model_id" in r).toBe(false);
  });

  it("includes voice_id and model_id when both are provided", () => {
    const r = buildSynthesizeRequest({
      text: "hello",
      provider: "openai",
      voiceId: "alloy",
      modelId: "tts-1",
    });
    expect(r).toEqual({ text: "hello", provider: "openai", voice_id: "alloy", model_id: "tts-1" });
  });

  it("includes only voice_id when modelId is absent", () => {
    const r = buildSynthesizeRequest({ text: "hi", provider: "edge", voiceId: "en-US-AriaNeural" });
    expect(r).toEqual({ text: "hi", provider: "edge", voice_id: "en-US-AriaNeural" });
    expect("model_id" in r).toBe(false);
  });
});

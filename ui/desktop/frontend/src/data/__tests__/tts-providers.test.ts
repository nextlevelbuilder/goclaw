/**
 * Desktop TTS provider catalog sanity tests — reduced shape (Phase C).
 * Mirrors ui/web/src/data/__tests__/tts-providers.test.ts
 */
import { describe, it, expect } from "vitest";
import {
  TTS_PROVIDERS,
  PROVIDER_MODEL_CATALOG,
  getProviderDefinition,
  type TtsProviderId,
} from "../tts-providers";

describe("Desktop TTS_PROVIDERS catalog (reduced shape)", () => {
  const ids: TtsProviderId[] = ["openai", "elevenlabs", "edge", "minimax", "gemini"];

  it("has 5 provider entries matching known IDs", () => {
    expect(Object.keys(TTS_PROVIDERS).sort()).toEqual([...ids].sort());
  });

  it.each(ids)("%s.id matches its record key", (id) => {
    expect(TTS_PROVIDERS[id].id).toBe(id);
  });

  it.each(ids)("%s has non-empty title and desc", (id) => {
    expect(TTS_PROVIDERS[id].title.length).toBeGreaterThan(0);
    expect(TTS_PROVIDERS[id].desc.length).toBeGreaterThan(0);
  });

  it.each(ids)("%s has a color string", (id) => {
    expect(typeof TTS_PROVIDERS[id].color).toBe("string");
    expect(TTS_PROVIDERS[id].color.length).toBeGreaterThan(0);
  });

  it("getProviderDefinition returns null for unknown id", () => {
    expect(getProviderDefinition("")).toBeNull();
    expect(getProviderDefinition("nonexistent")).toBeNull();
  });

  it("getProviderDefinition returns correct definition for known id", () => {
    expect(getProviderDefinition("openai")?.id).toBe("openai");
  });
});

describe("Desktop PROVIDER_MODEL_CATALOG (static fallback)", () => {
  it("ElevenLabs exposes all 4 backend-allowlisted models", () => {
    const modelIds = PROVIDER_MODEL_CATALOG.elevenlabs.map((m) => m.value);
    expect(modelIds).toContain("eleven_v3");
    expect(modelIds).toContain("eleven_flash_v2_5");
    expect(modelIds).toContain("eleven_multilingual_v2");
    expect(modelIds).toContain("eleven_turbo_v2_5");
    expect(modelIds).toHaveLength(4);
  });

  it("OpenAI models include the 3 standard model IDs", () => {
    const modelIds = PROVIDER_MODEL_CATALOG.openai.map((m) => m.value);
    expect(modelIds).toContain("gpt-4o-mini-tts");
    expect(modelIds).toContain("tts-1");
    expect(modelIds).toContain("tts-1-hd");
  });

  it("Edge has no models (voice-only provider)", () => {
    expect(PROVIDER_MODEL_CATALOG.edge).toHaveLength(0);
  });

  it("MiniMax has at least 2 models", () => {
    expect(PROVIDER_MODEL_CATALOG.minimax.length).toBeGreaterThanOrEqual(2);
    const ids = PROVIDER_MODEL_CATALOG.minimax.map((m) => m.value);
    expect(ids).toContain("speech-02-hd");
    expect(ids).toContain("speech-02-turbo");
  });
});
